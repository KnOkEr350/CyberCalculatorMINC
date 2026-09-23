package filestore

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// STORE-01: содержимое адресуется собственным SHA-256. Имя файла в хранилище
// больше не случайное: одинаковые байты занимают один blob, а путь проверяем
// по содержимому, а не по записи в БД.
const (
	blobRoot    = "blobs"
	blobStaging = "staging"
)

type Blob struct {
	// Path — абсолютный путь внутри хранилища, как его хранит attachments.
	Path   string
	SHA256 string
	Size   int64
	// Deduplicated — такие байты уже лежали в хранилище, новый файл не создан.
	Deduplicated bool
}

// randomName даёт имя временному файлу: до конца записи адрес содержимого
// ещё неизвестен.
func randomName() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// blobLocation раскладывает адрес по двум уровням: каталог из первых двух
// символов не даёт вырасти одному каталогу до сотен тысяч записей.
func blobLocation(sum string) (dir, path string) {
	dir = filepath.Join(blobRoot, sum[:2])
	return dir, filepath.Join(dir, sum)
}

func compressedBlobLocation(sum string) (dir, path string) {
	dir, path = blobLocation(sum)
	return dir, path + ".zst"
}

// IsBlobPath отличает адресуемый по содержимому файл от исторического,
// лежащего в каталоге мероприятия.
func IsBlobPath(root, path string) bool {
	rel, err := relative(root, path)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 3 || parts[0] != blobRoot {
		return false
	}
	name := strings.TrimSuffix(parts[2], ".zst")
	if len(name) != 64 || (parts[2] != name && parts[2] != name+".zst") || parts[1] != name[:2] {
		return false
	}
	_, err = hex.DecodeString(name)
	return err == nil
}

// CreateBlob записывает поток в хранилище потоково, считая SHA-256 на лету:
// содержимое не буферизуется целиком в памяти. Файл появляется по конечному
// адресу только целиком — до этого он лежит во временном каталоге, и
// оборванная запись не оставляет вида готового blob.
//
// limit ограничивает размер; при превышении файл не сохраняется.
func CreateBlob(root string, src io.Reader, limit int64) (Blob, error) {
	if limit <= 0 {
		return Blob{}, fmt.Errorf("недопустимый лимит размера")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return Blob{}, err
	}
	defer r.Close()
	for _, dir := range []string{blobRoot, filepath.Join(blobRoot, blobStaging)} {
		if err := r.Mkdir(dir, 0o750); err != nil && !os.IsExist(err) {
			return Blob{}, err
		}
	}

	name, err := randomName()
	if err != nil {
		return Blob{}, err
	}
	staging := filepath.Join(blobRoot, blobStaging, name)
	file, err := r.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return Blob{}, err
	}
	digest := sha256.New()
	// SHA-256 and Size describe the original bytes. The physical blob is zstd,
	// but callers and deduplication never depend on its compressed form.
	counter := &countingReader{reader: io.TeeReader(io.LimitReader(src, limit+1), digest)}
	encoder, encoderErr := zstd.NewWriter(file, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderCRC(true))
	var copyErr error
	if encoderErr == nil {
		_, copyErr = io.Copy(encoder, counter)
		copyErr = errors.Join(copyErr, encoder.Close())
	}
	size := counter.read
	syncErr := file.Sync()
	closeErr := file.Close()
	discard := func() {
		if removeErr := r.Remove(staging); removeErr != nil && !os.IsNotExist(removeErr) {
			// Незавершённый файл остаётся только во временном каталоге и
			// никогда не выглядит как готовый blob.
			_ = removeErr
		}
	}
	if encoderErr != nil || copyErr != nil || syncErr != nil || closeErr != nil {
		discard()
		return Blob{}, errors.Join(encoderErr, copyErr, syncErr, closeErr)
	}
	if size > limit {
		discard()
		return Blob{}, fmt.Errorf("превышен размер файла")
	}

	sum := hex.EncodeToString(digest.Sum(nil))
	dir, path := compressedBlobLocation(sum)
	if err := r.Mkdir(dir, 0o750); err != nil && !os.IsExist(err) {
		discard()
		return Blob{}, err
	}
	result := Blob{Path: filepath.Join(root, path), SHA256: sum, Size: size}
	if _, statErr := r.Stat(path); statErr == nil {
		// Такие байты уже лежат в хранилище: второй копии не создаём.
		discard()
		if err := VerifyBlob(root, result.Path); err != nil {
			return Blob{}, fmt.Errorf("существующий blob повреждён: %w", err)
		}
		result.Deduplicated = true
		return result, nil
	}
	// Compatibility: a blob written before STORE-03 has the same plaintext
	// address without the .zst suffix and remains the canonical copy.
	_, legacyPath := blobLocation(sum)
	if _, statErr := r.Stat(legacyPath); statErr == nil {
		discard()
		result.Path = filepath.Join(root, legacyPath)
		if err := VerifyBlob(root, result.Path); err != nil {
			return Blob{}, fmt.Errorf("существующий legacy blob повреждён: %w", err)
		}
		result.Deduplicated = true
		return result, nil
	}
	// Переименование внутри одной файловой системы атомарно: конкурирующая
	// запись тех же байтов не может дать половину файла.
	if err := r.Rename(staging, path); err != nil {
		if _, statErr := r.Stat(path); statErr == nil {
			discard()
			result.Deduplicated = true
			return result, nil
		}
		discard()
		return Blob{}, err
	}
	return result, nil
}

// VerifyBlob перечитывает файл и сверяет его с адресом: адресация по
// содержимому даёт проверку целостности без отдельного поля.
func VerifyBlob(root, path string) error {
	rel, err := relative(root, path)
	if err != nil {
		return err
	}
	if !IsBlobPath(root, path) {
		return fmt.Errorf("файл не адресуется по содержимому")
	}
	file, err := Open(root, path)
	if err != nil {
		return err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return err
	}
	want := strings.TrimSuffix(filepath.Base(rel), ".zst")
	if got := hex.EncodeToString(digest.Sum(nil)); got != want {
		return fmt.Errorf("содержимое не соответствует адресу: %s", got)
	}
	return nil
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.read += int64(n)
	return n, err
}
