// Package filestore confines all file operations to the upload root.
package filestore

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

const maxDecodedBlobSize = 20 << 20

// ReadSeekFile is the common view of both legacy plain files and compressed
// CAS blobs. Callers such as the Office validator need random access, while
// downloads and the antivirus pipeline only stream it.
type ReadSeekFile interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
}

func relative(root, path string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	file, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return "", fmt.Errorf("путь вне хранилища")
	}
	return rel, nil
}

func Open(root, path string) (ReadSeekFile, error) {
	rel, err := relative(root, path)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	file, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	if filepath.Ext(rel) != ".zst" || !IsBlobPath(root, path) {
		return file, nil
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	decoder, err := zstd.NewReader(file, zstd.WithDecoderMaxMemory(maxDecodedBlobSize*2))
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("повреждённый zstd blob: %w", err)
	}
	decoded, err := io.ReadAll(io.LimitReader(decoder, maxDecodedBlobSize+1))
	decoder.Close()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("ошибка распаковки zstd blob: %w", err)
	}
	if len(decoded) > maxDecodedBlobSize {
		file.Close()
		return nil, fmt.Errorf("распакованный blob превышает лимит")
	}
	return &memoryFile{Reader: bytes.NewReader(decoded), file: file, info: decodedFileInfo{FileInfo: info, size: int64(len(decoded))}}, nil
}

type memoryFile struct {
	*bytes.Reader
	file *os.File
	info os.FileInfo
}

func (f *memoryFile) Close() error               { return f.file.Close() }
func (f *memoryFile) Stat() (os.FileInfo, error) { return f.info, nil }

type decodedFileInfo struct {
	os.FileInfo
	size int64
}

func (i decodedFileInfo) Size() int64 { return i.size }

func Create(root, entry, name string) (*os.File, string, error) {
	if !filepath.IsLocal(entry) || filepath.Base(entry) != entry || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return nil, "", fmt.Errorf("недопустимое имя хранилища")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	if err := r.Mkdir(entry, 0750); err != nil && !os.IsExist(err) {
		return nil, "", err
	}
	path := filepath.Join(entry, name)
	f, err := r.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	return f, filepath.Join(root, path), err
}

func Remove(root, path string) error {
	rel, err := relative(root, path)
	if err != nil {
		return err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.Remove(rel)
}
