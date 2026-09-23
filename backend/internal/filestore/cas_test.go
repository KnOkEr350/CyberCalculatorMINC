package filestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// STORE-01: адрес файла — это его содержимое. Одинаковые байты занимают один
// blob, разные — разные, а адрес совпадает с SHA-256 потока.
func TestBlobIsAddressedByItsContent(t *testing.T) {
	root := t.TempDir()
	content := []byte("акт сдачи-приёмки работ")
	want := sha256.Sum256(content)

	first, err := CreateBlob(root, bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if first.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("адрес %s не совпадает с SHA-256 содержимого", first.SHA256)
	}
	if first.Size != int64(len(content)) {
		t.Fatalf("размер %d, ожидалось %d", first.Size, len(content))
	}
	if first.Deduplicated {
		t.Fatal("первая запись не может быть дедупликацией")
	}
	if !IsBlobPath(root, first.Path) {
		t.Fatalf("путь %q не выглядит адресуемым по содержимому", first.Path)
	}
	if err := VerifyBlob(root, first.Path); err != nil {
		t.Fatalf("свежий blob должен проходить проверку: %v", err)
	}

	// Те же байты — тот же адрес и ни одного лишнего файла.
	second, err := CreateBlob(root, bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if second.Path != first.Path || second.SHA256 != first.SHA256 {
		t.Fatalf("одинаковое содержимое дало разные адреса: %s и %s", first.Path, second.Path)
	}
	if !second.Deduplicated {
		t.Fatal("повторная запись тех же байтов должна дедуплицироваться")
	}
	if got := countBlobs(t, root); got != 1 {
		t.Fatalf("в хранилище %d файлов, ожидался один", got)
	}

	// Другие байты — другой адрес.
	other, err := CreateBlob(root, strings.NewReader("другой документ"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if other.Path == first.Path {
		t.Fatal("разное содержимое не может делить адрес")
	}
	if got := countBlobs(t, root); got != 2 {
		t.Fatalf("в хранилище %d файлов, ожидалось два", got)
	}
}

// STORE-03: CAS хранит zstd, но адрес, размер и выдаваемые байты относятся к
// исходному документу. Формат хранения не протекает в API вложений.
func TestCompressedBlobRoundTripAndPlaintextHash(t *testing.T) {
	root := t.TempDir()
	content := bytes.Repeat([]byte("строка подтверждающего документа\n"), 4096)
	want := sha256.Sum256(content)

	blob, err := CreateBlob(root, bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(blob.Path) != ".zst" {
		t.Fatalf("новый blob должен храниться как zstd: %s", blob.Path)
	}
	if blob.SHA256 != hex.EncodeToString(want[:]) || blob.Size != int64(len(content)) {
		t.Fatalf("метаданные должны описывать исходные байты: %+v", blob)
	}
	physical, err := os.Stat(blob.Path)
	if err != nil {
		t.Fatal(err)
	}
	if physical.Size() >= int64(len(content)) {
		t.Fatalf("повторяющийся документ не сжат: %d >= %d", physical.Size(), len(content))
	}
	file, err := Open(root, blob.Path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(file)
	file.Close()
	if err != nil || !bytes.Equal(decoded, content) {
		t.Fatalf("round-trip zstd изменил документ: %v", err)
	}
}

// Уже существующие CAS-файлы без сжатия продолжают читаться и участвовать в
// дедупликации: rollout STORE-03 не требует одномоментной перезаписи архива.
func TestLegacyPlainBlobRemainsCompatible(t *testing.T) {
	root := t.TempDir()
	content := []byte("исторический несжатый документ")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	dir, rel := blobLocation(hash)
	if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(root, rel)
	if err := os.WriteFile(legacyPath, content, 0o640); err != nil {
		t.Fatal(err)
	}

	blob, err := CreateBlob(root, bytes.NewReader(content), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !blob.Deduplicated || blob.Path != legacyPath || blob.Size != int64(len(content)) {
		t.Fatalf("legacy blob не переиспользован: %+v", blob)
	}
	if err := VerifyBlob(root, legacyPath); err != nil {
		t.Fatalf("legacy blob перестал проходить проверку: %v", err)
	}
}

// Файл появляется по своему адресу только целиком: при обрыве потока по
// конечному пути не остаётся ничего, что выглядело бы готовым документом.
func TestInterruptedBlobNeverAppearsAtItsAddress(t *testing.T) {
	root := t.TempDir()
	content := []byte("половина документа обрывается")
	want := sha256.Sum256(content)
	_, path := compressedBlobLocation(hex.EncodeToString(want[:]))

	_, err := CreateBlob(root, io.MultiReader(bytes.NewReader(content[:10]), failingReader{}), 1<<20)
	if err == nil {
		t.Fatal("оборванный поток не должен давать blob")
	}
	if _, statErr := os.Stat(filepath.Join(root, path)); !os.IsNotExist(statErr) {
		t.Fatal("по адресу оборванного файла не должно быть ничего")
	}
	if got := countBlobs(t, root); got != 0 {
		t.Fatalf("после обрыва в хранилище осталось %d файлов", got)
	}
}

// Превышение лимита не сохраняет файл: хранилище не принимает то, что
// отвергает обработчик.
func TestBlobRejectsOversizedStream(t *testing.T) {
	root := t.TempDir()
	if _, err := CreateBlob(root, bytes.NewReader(make([]byte, 101)), 100); err == nil {
		t.Fatal("файл больше лимита не должен сохраняться")
	}
	if got := countBlobs(t, root); got != 0 {
		t.Fatalf("после отказа в хранилище осталось %d файлов", got)
	}
	// Ровно по лимиту — допустимо.
	if _, err := CreateBlob(root, bytes.NewReader(make([]byte, 100)), 100); err != nil {
		t.Fatalf("файл ровно по лимиту должен приниматься: %v", err)
	}
}

// Одновременная загрузка одинаковых байтов даёт один файл и ни одной
// половинчатой записи: переименование в конечный адрес атомарно.
func TestConcurrentBlobWritesConvergeOnOneFile(t *testing.T) {
	root := t.TempDir()
	content := []byte("одновременно загруженный приказ о назначении наставника")
	const writers = 12

	var wg sync.WaitGroup
	paths := make([]string, writers)
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blob, err := CreateBlob(root, bytes.NewReader(content), 1<<20)
			paths[index], errs[index] = blob.Path, err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("запись %d завершилась ошибкой: %v", i, err)
		}
		if paths[i] != paths[0] {
			t.Fatalf("запись %d дала другой адрес: %s против %s", i, paths[i], paths[0])
		}
	}
	if got := countBlobs(t, root); got != 1 {
		t.Fatalf("параллельные записи оставили %d файлов вместо одного", got)
	}
	if err := VerifyBlob(root, paths[0]); err != nil {
		t.Fatalf("итоговый файл повреждён: %v", err)
	}
}

// Подменённые на диске байты обнаруживаются сверкой с адресом.
func TestVerifyBlobDetectsTamperedContent(t *testing.T) {
	root := t.TempDir()
	blob, err := CreateBlob(root, strings.NewReader("подтверждающий документ"), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob.Path, []byte("подменённое содержимое"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBlob(root, blob.Path); err == nil {
		t.Fatal("подмена содержимого должна обнаруживаться сверкой с адресом")
	}
	// Исторический файл вне CAS честно отличается от адресуемого.
	legacy, legacyPath, err := Create(root, "entry", "legacy.bin")
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	if IsBlobPath(root, legacyPath) {
		t.Fatal("файл из каталога мероприятия не адресуется по содержимому")
	}
	if err := VerifyBlob(root, legacyPath); err == nil {
		t.Fatal("для файла вне CAS проверка адреса невозможна и должна возвращать ошибку")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// countBlobs считает готовые файлы, не заглядывая во временный каталог.
func countBlobs(t *testing.T, root string) int {
	t.Helper()
	count := 0
	staging := filepath.Join(root, blobRoot, blobStaging)
	err := filepath.WalkDir(filepath.Join(root, blobRoot), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			if path == staging {
				return filepath.SkipDir
			}
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}
