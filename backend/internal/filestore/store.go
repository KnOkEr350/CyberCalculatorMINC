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
