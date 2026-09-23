// Package backup создаёт и проверяет шифрованную резервную копию инстанса:
// база данных, хранилище вложений и конфигурация в одном файле (OPS-10).
//
// Копия потоковая: дамп базы не ложится на диск открытым текстом, память не
// зависит от размера данных. Каждый файл сопровождается контрольной суммой
// SHA-256, итоговый манифест перечисляет всё, что вошло в копию, и факты о
// данных (число записей, голова цепочки аудита) для проверки восстановления.
package backup

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Пути внутри копии.
const (
	DatabaseEntry = "database.dump"
	uploadsPrefix = "uploads/"
	configPrefix  = "config/"
)

const (
	recordFileStart = 1
	recordData      = 2
	recordFileEnd   = 3
	recordManifest  = 4

	maxPathLength   = 1024
	maxManifestSize = 16 << 20
	// Предел числа файлов и размера одного куска данных защищает восстановление
	// от испорченной, но правильно зашифрованной копии.
	maxFiles = 5_000_000
)

var (
	ErrManifest   = errors.New("backup: манифест не совпадает с содержимым копии")
	ErrPath       = errors.New("backup: недопустимый путь в копии")
	ErrIncomplete = errors.New("backup: в копии нет обязательного файла")
)

// FileEntry — файл в копии.
type FileEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest — опись копии.
type Manifest struct {
	Format     int               `json:"format"`
	CreatedAt  time.Time         `json:"created_at"`
	AppVersion string            `json:"app_version"`
	Facts      map[string]string `json:"facts,omitempty"`
	Files      []FileEntry       `json:"files"`
}

// Source — что копировать.
type Source struct {
	// Dump пишет дамп базы; без него копия не создаётся.
	Dump func(context.Context, io.Writer) error
	// UploadsDir — каталог хранилища вложений (может отсутствовать).
	UploadsDir string
	// ConfigFiles — имя в копии → путь на диске.
	ConfigFiles map[string]string
	AppVersion  string
	Facts       map[string]string
	// Rounds — число итераций PBKDF2; ноль означает рабочее значение.
	Rounds int
	Now    func() time.Time
}

// ValidEntryPath — путь внутри копии: относительный, без «..», без служебных
// символов. Проверяется и при создании, и при восстановлении.
func ValidEntryPath(p string) bool {
	if p == "" || len(p) > maxPathLength || strings.ContainsAny(p, "\x00\\") || path.IsAbs(p) {
		return false
	}
	if path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

type recordWriter struct {
	w io.Writer
}

func (r recordWriter) header(kind byte, payload []byte) error {
	var head [5]byte
	head[0] = kind
	binary.BigEndian.PutUint32(head[1:], uint32(len(payload)))
	if _, err := r.w.Write(head[:]); err != nil {
		return err
	}
	_, err := r.w.Write(payload)
	return err
}

// file пишет один файл потоком и возвращает его опись.
func (r recordWriter) file(name string, write func(io.Writer) error) (FileEntry, error) {
	if !ValidEntryPath(name) {
		return FileEntry{}, fmt.Errorf("%w: %q", ErrPath, name)
	}
	if err := r.header(recordFileStart, []byte(name)); err != nil {
		return FileEntry{}, err
	}
	hash := sha256.New()
	dst := &dataWriter{rw: r, hash: hash}
	if err := write(dst); err != nil {
		return FileEntry{}, err
	}
	sum := hash.Sum(nil)
	end := make([]byte, 0, 40)
	end = binary.BigEndian.AppendUint64(end, uint64(dst.size))
	end = append(end, sum...)
	if err := r.header(recordFileEnd, end); err != nil {
		return FileEntry{}, err
	}
	return FileEntry{Path: name, Size: dst.size, SHA256: hex.EncodeToString(sum)}, nil
}

type dataWriter struct {
	rw   recordWriter
	hash interface{ Write([]byte) (int, error) }
	size int64
}

func (d *dataWriter) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n := min(len(p), 64<<10)
		if err := d.rw.header(recordData, p[:n]); err != nil {
			return total, err
		}
		d.hash.Write(p[:n])
		d.size += int64(n)
		total += n
		p = p[n:]
	}
	return total, nil
}

// Create пишет копию в out и возвращает манифест.
func Create(ctx context.Context, out io.Writer, passphrase string, src Source) (Manifest, error) {
	if src.Dump == nil {
		return Manifest{}, errors.New("backup: не задан источник дампа базы")
	}
	rounds := src.Rounds
	if rounds == 0 {
		rounds = defaultRounds
	}
	sealer, err := newSealWriter(out, passphrase, rounds, defaultChunk)
	if err != nil {
		return Manifest{}, err
	}
	now := time.Now
	if src.Now != nil {
		now = src.Now
	}
	manifest := Manifest{Format: 1, CreatedAt: now().UTC(), AppVersion: src.AppVersion, Facts: src.Facts}
	rw := recordWriter{w: sealer}

	entry, err := rw.file(DatabaseEntry, func(w io.Writer) error { return src.Dump(ctx, w) })
	if err != nil {
		return Manifest{}, fmt.Errorf("дамп базы: %w", err)
	}
	if entry.Size == 0 {
		return Manifest{}, errors.New("backup: дамп базы пуст")
	}
	manifest.Files = append(manifest.Files, entry)

	if src.UploadsDir != "" {
		if err := copyTree(ctx, rw, src.UploadsDir, uploadsPrefix, &manifest); err != nil {
			return Manifest{}, err
		}
	}
	names := make([]string, 0, len(src.ConfigFiles))
	for name := range src.ConfigFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, err := rw.file(configPrefix+name, func(w io.Writer) error { return copyFile(ctx, w, src.ConfigFiles[name]) })
		if err != nil {
			return Manifest{}, fmt.Errorf("конфигурация %s: %w", name, err)
		}
		manifest.Files = append(manifest.Files, entry)
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if err := rw.header(recordManifest, encoded); err != nil {
		return Manifest{}, err
	}
	if err := sealer.Close(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func copyFile(ctx context.Context, w io.Writer, source string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// copyTree копирует обычные файлы каталога; символические ссылки и файлы
// особых типов не копируются, а прерывают копию: молча пропущенный файл —
// это тихая потеря данных.
func copyTree(ctx context.Context, rw recordWriter, root, prefix string, manifest *Manifest) error {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("backup: %s не каталог", root)
	}
	var files []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("backup: %s — не обычный файл (ссылка или особый тип)", p)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	if len(files) > maxFiles {
		return fmt.Errorf("backup: слишком много файлов: %d", len(files))
	}
	for _, rel := range files {
		entry, err := rw.file(prefix+rel, func(w io.Writer) error { return copyFile(ctx, w, filepath.Join(root, filepath.FromSlash(rel))) })
		if err != nil {
			return fmt.Errorf("файл %s: %w", rel, err)
		}
		manifest.Files = append(manifest.Files, entry)
	}
	return nil
}

// Sink принимает восстанавливаемые файлы. Verify использует пустой приёмник.
type Sink interface {
	Begin(path string) (io.WriteCloser, error)
	// Abort вызывается при ошибке: приёмник обязан убрать всё, что успел записать.
	Abort()
	Commit() error
}

type discardSink struct{}

func (discardSink) Begin(string) (io.WriteCloser, error) { return nopCloser{io.Discard}, nil }
func (discardSink) Abort()                               {}
func (discardSink) Commit() error                        { return nil }

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// Read разбирает копию, проверяет подлинность, контрольные суммы и полноту и
// отдаёт файлы приёмнику. Ошибка на любом этапе отменяет приёмник.
func Read(in io.Reader, passphrase string, sink Sink) (Manifest, error) {
	manifest, err := read(in, passphrase, sink)
	if err != nil {
		sink.Abort()
		return Manifest{}, err
	}
	if err := sink.Commit(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Verify проверяет копию, ничего не сохраняя.
func Verify(in io.Reader, passphrase string) (Manifest, error) {
	return Read(in, passphrase, discardSink{})
}

func read(in io.Reader, passphrase string, sink Sink) (Manifest, error) {
	plain, err := newOpenReader(in, passphrase)
	if err != nil {
		return Manifest{}, err
	}
	var seen []FileEntry
	var current io.WriteCloser
	var currentHash = sha256.New()
	var currentName string
	var currentSize int64
	names := map[string]bool{}
	var manifest Manifest
	haveManifest := false
	for {
		var head [5]byte
		if _, err := io.ReadFull(plain, head[:]); err != nil {
			if err == io.EOF && haveManifest {
				break
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return Manifest{}, ErrTruncated
			}
			return Manifest{}, err
		}
		if haveManifest {
			return Manifest{}, fmt.Errorf("%w: данные после манифеста", ErrManifest)
		}
		kind, size := head[0], binary.BigEndian.Uint32(head[1:])
		limit := uint32(64 << 10)
		switch kind {
		case recordFileStart:
			limit = maxPathLength
		case recordFileEnd:
			limit = 40
		case recordManifest:
			limit = maxManifestSize
		}
		if size > limit {
			return Manifest{}, fmt.Errorf("%w: запись %d длиной %d", ErrFormat, kind, size)
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(plain, payload); err != nil {
			return Manifest{}, ErrTruncated
		}
		switch kind {
		case recordFileStart:
			if current != nil {
				return Manifest{}, fmt.Errorf("%w: файл начат до завершения предыдущего", ErrFormat)
			}
			currentName = string(payload)
			if !ValidEntryPath(currentName) {
				return Manifest{}, fmt.Errorf("%w: %q", ErrPath, currentName)
			}
			if names[currentName] {
				return Manifest{}, fmt.Errorf("%w: файл %q повторяется", ErrManifest, currentName)
			}
			if len(names) >= maxFiles {
				return Manifest{}, fmt.Errorf("%w: слишком много файлов", ErrFormat)
			}
			names[currentName] = true
			if current, err = sink.Begin(currentName); err != nil {
				return Manifest{}, err
			}
			currentHash.Reset()
			currentSize = 0
		case recordData:
			if current == nil {
				return Manifest{}, fmt.Errorf("%w: данные вне файла", ErrFormat)
			}
			if _, err := current.Write(payload); err != nil {
				return Manifest{}, err
			}
			currentHash.Write(payload)
			currentSize += int64(len(payload))
		case recordFileEnd:
			if current == nil || len(payload) != 40 {
				return Manifest{}, fmt.Errorf("%w: конец файла без начала", ErrFormat)
			}
			if err := current.Close(); err != nil {
				return Manifest{}, err
			}
			current = nil
			declared := int64(binary.BigEndian.Uint64(payload[:8]))
			sum := currentHash.Sum(nil)
			if declared != currentSize || hex.EncodeToString(sum) != hex.EncodeToString(payload[8:]) {
				return Manifest{}, fmt.Errorf("%w: контрольная сумма файла %s не сходится", ErrManifest, currentName)
			}
			seen = append(seen, FileEntry{Path: currentName, Size: currentSize, SHA256: hex.EncodeToString(sum)})
		case recordManifest:
			if current != nil {
				return Manifest{}, fmt.Errorf("%w: манифест внутри файла", ErrFormat)
			}
			if err := json.Unmarshal(payload, &manifest); err != nil {
				return Manifest{}, fmt.Errorf("%w: %v", ErrManifest, err)
			}
			haveManifest = true
		default:
			return Manifest{}, fmt.Errorf("%w: неизвестный тип записи %d", ErrFormat, kind)
		}
	}
	if current != nil {
		return Manifest{}, ErrTruncated
	}
	if manifest.Format != 1 {
		return Manifest{}, fmt.Errorf("%w: формат %d", ErrManifest, manifest.Format)
	}
	// Манифест должен описывать ровно то, что пришло, в том же порядке.
	if len(manifest.Files) != len(seen) {
		return Manifest{}, fmt.Errorf("%w: в манифесте %d файлов, в копии %d", ErrManifest, len(manifest.Files), len(seen))
	}
	for i := range seen {
		if manifest.Files[i] != seen[i] {
			return Manifest{}, fmt.Errorf("%w: файл %s", ErrManifest, seen[i].Path)
		}
	}
	if !names[DatabaseEntry] {
		return Manifest{}, fmt.Errorf("%w: %s", ErrIncomplete, DatabaseEntry)
	}
	return manifest, nil
}
