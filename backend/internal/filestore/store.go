// Package filestore confines all file operations to the upload root.
package filestore

import (
	"fmt"
	"os"
	"path/filepath"
)

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

func Open(root, path string) (*os.File, error) {
	rel, err := relative(root, path)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Open(rel)
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
