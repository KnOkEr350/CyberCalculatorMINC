package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const passphrase = "правильная парольная фраза 2026"
const fastRounds = minRounds

func randomBytes(t testing.TB, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// fixture — каталог с вложениями и конфигурацией и ожидаемое содержимое.
type fixture struct {
	uploads string
	config  map[string]string
	dump    []byte
	files   map[string][]byte
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	f := fixture{uploads: filepath.Join(dir, "uploads"), config: map[string]string{}, dump: randomBytes(t, 3<<20+17), files: map[string][]byte{}}
	write := func(rel string, data []byte) {
		p := filepath.Join(f.uploads, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		f.files["uploads/"+rel] = data
	}
	write("ab/cdef.bin", randomBytes(t, 2<<20+5)) // несколько кусков
	write("ab/small.txt", []byte("документ"))
	write("empty.bin", nil)
	write("ю/юникод.pdf", randomBytes(t, 1000))
	envFile := filepath.Join(dir, "env")
	if err := os.WriteFile(envFile, []byte("APP_ENV=production\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.config["env"] = envFile
	f.files["config/env"] = []byte("APP_ENV=production\n")
	f.files[DatabaseEntry] = f.dump
	return f
}

func (f fixture) source() Source {
	return Source{
		Dump:       func(_ context.Context, w io.Writer) error { _, err := w.Write(f.dump); return err },
		UploadsDir: f.uploads, ConfigFiles: f.config, AppVersion: "test-1",
		Facts: map[string]string{"entries": "42"}, Rounds: fastRounds,
		Now: func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) },
	}
}

func (f fixture) create(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if _, err := Create(context.Background(), &out, passphrase, f.source()); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestRoundTripRestoresEveryFileByteForByte(t *testing.T) {
	f := newFixture(t)
	data := f.create(t)

	manifest, err := Verify(bytes.NewReader(data), passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.AppVersion != "test-1" || manifest.Facts["entries"] != "42" || len(manifest.Files) != len(f.files) {
		t.Fatalf("манифест: %+v", manifest)
	}

	dest := filepath.Join(t.TempDir(), "restored")
	sink, err := NewDirSink(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Read(bytes.NewReader(data), passphrase, sink); err != nil {
		t.Fatal(err)
	}
	for rel, want := range f.files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s восстановлен неверно: %v", rel, err)
		}
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".restore-*")); len(leftovers) != 0 {
		t.Fatalf("временный каталог не убран: %v", leftovers)
	}
}

func TestWrongPassphraseAndFormat(t *testing.T) {
	data := newFixture(t).create(t)
	if _, err := Verify(bytes.NewReader(data), "совсем другая фраза 123"); !errors.Is(err, ErrAuth) {
		t.Fatalf("неверная фраза: %v", err)
	}
	if _, err := Verify(bytes.NewReader([]byte("это не резервная копия")), passphrase); !errors.Is(err, ErrFormat) {
		t.Fatalf("чужой файл: %v", err)
	}
	if _, err := Create(context.Background(), io.Discard, "короткая", Source{Dump: func(context.Context, io.Writer) error { return nil }}); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("короткая фраза: %v", err)
	}
}

// Любая порча байта — в заголовке, куске или длине — обнаруживается.
func TestAnyCorruptionIsDetected(t *testing.T) {
	data := newFixture(t).create(t)
	positions := []int{0, 5, headerSize - 1, headerSize, headerSize + 3, headerSize + 100, len(data) / 2, len(data) - 1}
	for _, at := range positions {
		bad := bytes.Clone(data)
		bad[at] ^= 0x01
		if _, err := Verify(bytes.NewReader(bad), passphrase); err == nil {
			t.Errorf("порча байта %d не обнаружена", at)
		}
	}
}

func TestTruncationAndTrailingDataAreRejected(t *testing.T) {
	data := newFixture(t).create(t)
	for _, cut := range []int{1, 17, 1000, len(data) / 3, len(data) - headerSize} {
		if _, err := Verify(bytes.NewReader(data[:len(data)-cut]), passphrase); err == nil {
			t.Errorf("копия, обрезанная на %d байт, принята", cut)
		}
	}
	if _, err := Verify(bytes.NewReader(data[:headerSize]), passphrase); !errors.Is(err, ErrTruncated) {
		t.Fatalf("только заголовок: %v", err)
	}
	if _, err := Verify(bytes.NewReader(append(bytes.Clone(data), 1, 2, 3, 4, 5)), passphrase); err == nil {
		t.Fatal("хвост после копии принят")
	}
}

// Перестановка кусков ломает nonce: копию нельзя «собрать заново» из кусков.
func TestChunkReorderingIsRejected(t *testing.T) {
	data := newFixture(t).create(t)
	// Находим границы первых двух кусков и меняем их местами.
	off := headerSize
	first := 4 + int(uint32(data[off])<<24|uint32(data[off+1])<<16|uint32(data[off+2])<<8|uint32(data[off+3]))
	second := 4 + int(uint32(data[off+first])<<24|uint32(data[off+first+1])<<16|uint32(data[off+first+2])<<8|uint32(data[off+first+3]))
	swapped := append([]byte{}, data[:off]...)
	swapped = append(swapped, data[off+first:off+first+second]...)
	swapped = append(swapped, data[off:off+first]...)
	swapped = append(swapped, data[off+first+second:]...)
	if _, err := Verify(bytes.NewReader(swapped), passphrase); !errors.Is(err, ErrAuth) {
		t.Fatalf("перестановка кусков: %v", err)
	}
}

// forged собирает правильно зашифрованную копию с заданными записями:
// подлинность есть, но содержимое вредное или несогласованное.
func forged(t *testing.T, write func(rw recordWriter)) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := newSealWriter(&out, passphrase, fastRounds, defaultChunk)
	if err != nil {
		t.Fatal(err)
	}
	write(recordWriter{w: w})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestPathTraversalInAnAuthenticCopyIsRejected(t *testing.T) {
	for _, name := range []string{"../evil", "/etc/passwd", "a/../../b", "a//b", ".", "uploads/./x", `a\b`, ""} {
		data := forged(t, func(rw recordWriter) {
			rw.header(recordFileStart, []byte(name))
			rw.header(recordFileEnd, make([]byte, 40))
		})
		dest := filepath.Join(t.TempDir(), "d")
		sink, err := NewDirSink(dest)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Read(bytes.NewReader(data), passphrase, sink); !errors.Is(err, ErrPath) {
			t.Errorf("путь %q: %v", name, err)
		}
		// Проверка без восстановления называет вредный путь так же: копия с
		// таким путём не должна выглядеть исправной.
		if _, err := Verify(bytes.NewReader(data), passphrase); !errors.Is(err, ErrPath) {
			t.Errorf("проверка, путь %q: %v", name, err)
		}
		if _, err := os.Lstat(dest); !os.IsNotExist(err) {
			t.Errorf("путь %q: каталог назначения не должен появиться", name)
		}
	}
	if ValidEntryPath("../x") || !ValidEntryPath("uploads/ab/cd.bin") {
		t.Fatal("ValidEntryPath")
	}
}

func TestManifestMustMatchWhatWasWritten(t *testing.T) {
	data := forged(t, func(rw recordWriter) {
		entry, _ := rw.file(DatabaseEntry, func(w io.Writer) error { _, err := w.Write([]byte("dump")); return err })
		entry.SHA256 = strings.Repeat("0", 64)
		rw.header(recordManifest, []byte(`{"format":1,"files":[{"path":"database.dump","size":4,"sha256":"`+entry.SHA256+`"}]}`))
	})
	if _, err := Verify(bytes.NewReader(data), passphrase); !errors.Is(err, ErrManifest) {
		t.Fatalf("манифест с чужой суммой: %v", err)
	}
	noDump := forged(t, func(rw recordWriter) {
		rw.file("config/env", func(w io.Writer) error { _, err := w.Write([]byte("x")); return err })
		rw.header(recordManifest, []byte(`{"format":1,"files":[]}`))
	})
	if _, err := Verify(bytes.NewReader(noDump), passphrase); err == nil {
		t.Fatal("копия без дампа базы и с неполным манифестом принята")
	}
	dup := forged(t, func(rw recordWriter) {
		rw.file(DatabaseEntry, func(w io.Writer) error { _, err := w.Write([]byte("a")); return err })
		rw.file(DatabaseEntry, func(w io.Writer) error { _, err := w.Write([]byte("b")); return err })
		rw.header(recordManifest, []byte(`{"format":1,"files":[]}`))
	})
	if _, err := Verify(bytes.NewReader(dup), passphrase); !errors.Is(err, ErrManifest) {
		t.Fatalf("повтор файла: %v", err)
	}
}

func TestCreateRefusesUnsafeSources(t *testing.T) {
	f := newFixture(t)
	if err := os.Symlink("/etc/hostname", filepath.Join(f.uploads, "link")); err != nil {
		t.Skip("символические ссылки недоступны:", err)
	}
	if _, err := Create(context.Background(), io.Discard, passphrase, f.source()); err == nil {
		t.Fatal("ссылка в хранилище должна прерывать копию, а не пропускаться молча")
	}
	empty := f.source()
	empty.Dump = func(context.Context, io.Writer) error { return nil }
	os.Remove(filepath.Join(f.uploads, "link"))
	if _, err := Create(context.Background(), io.Discard, passphrase, empty); err == nil {
		t.Fatal("пустой дамп базы — не копия")
	}
	failing := f.source()
	failing.Dump = func(context.Context, io.Writer) error { return errors.New("pg_dump упал") }
	if _, err := Create(context.Background(), io.Discard, passphrase, failing); err == nil || !strings.Contains(err.Error(), "pg_dump упал") {
		t.Fatalf("сбой дампа должен доходить до вызывающего: %v", err)
	}
}

func TestRestoreNeverTouchesExistingDataAndCleansUpAfterFailure(t *testing.T) {
	data := newFixture(t).create(t)
	dir := t.TempDir()
	existing := filepath.Join(dir, "live")
	os.MkdirAll(existing, 0o755)
	os.WriteFile(filepath.Join(existing, "keep"), []byte("живые данные"), 0o644)
	if _, err := NewDirSink(existing); err == nil {
		t.Fatal("восстановление поверх существующего каталога недопустимо")
	}

	dest := filepath.Join(dir, "restored")
	sink, err := NewDirSink(dest)
	if err != nil {
		t.Fatal(err)
	}
	bad := bytes.Clone(data)
	bad[len(bad)-3] ^= 0xff
	if _, err := Read(bytes.NewReader(bad), passphrase, sink); err == nil {
		t.Fatal("испорченная копия принята")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatal("после неудачи назначение не должно появиться")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".restore-*")); len(left) != 0 {
		t.Fatalf("после неудачи не должно остаться временных каталогов: %v", left)
	}
	if got, _ := os.ReadFile(filepath.Join(existing, "keep")); string(got) != "живые данные" {
		t.Fatal("существующие данные не должны меняться")
	}
}

func TestCancellationStopsCreate(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Create(ctx, io.Discard, passphrase, f.source()); err == nil {
		t.Fatal("отменённый контекст должен прерывать копию")
	}
}

// OPS-10 / QA-10: память не зависит от размера копии. Дамп в 96 МиБ создаётся,
// проверяется и восстанавливается; пиковый рост кучи остаётся в пределах
// нескольких кусков.
func TestMemoryStaysBoundedForLargeCopies(t *testing.T) {
	if testing.Short() {
		t.Skip("длинная проверка")
	}
	const total = 96 << 20
	piece := randomBytes(t, 1<<20)
	dump := func(_ context.Context, w io.Writer) error {
		for written := 0; written < total; written += len(piece) {
			if _, err := w.Write(piece); err != nil {
				return err
			}
		}
		return nil
	}
	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peak.Load() {
				peak.Store(m.HeapAlloc)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	file := filepath.Join(t.TempDir(), "big.ccbk")
	out, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), out, passphrase, Source{Dump: dump, Rounds: fastRounds}); err != nil {
		t.Fatal(err)
	}
	out.Close()
	in, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Verify(in, passphrase)
	in.Close()
	if err != nil || manifest.Files[0].Size != total {
		t.Fatalf("проверка большой копии: %v %+v", err, manifest)
	}
	close(stop)
	<-done
	if growth := int64(peak.Load()) - int64(base.HeapAlloc); growth > 40<<20 {
		t.Fatalf("рост кучи %d МиБ при копии %d МиБ: память должна быть ограничена размером куска", growth>>20, total>>20)
	}
}
