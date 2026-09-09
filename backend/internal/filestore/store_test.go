package filestore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorageConfinement(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{secret, filepath.Join(root, "escape", "secret")} {
		if f, err := Open(root, path); err == nil {
			f.Close()
			t.Fatal("escaped storage")
		}
		if err := Remove(root, path); err == nil {
			t.Fatal("removed external file")
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatal("external file lost")
	}
	for _, entry := range []string{"escape", "../outside", "/absolute"} {
		if f, _, err := Create(root, entry, "new-file"); err == nil {
			f.Close()
			t.Fatal("created file outside storage", entry)
		}
	}
	f, path, err := Create(root, "entry", "file")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if f, _, err := Create(root, "entry", "file"); err == nil {
		f.Close()
		t.Fatal("overwrote existing file")
	}
	if err := Remove(root, path); err != nil {
		t.Fatal(err)
	}
}

func TestContentValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(path, []byte("<script>alert(1)</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"document.pdf", "document.txt", "program.exe", "file.html", "bad\n.txt"} {
		if Validate(name, path) == nil {
			t.Fatal("dangerous content accepted", name)
		}
	}
	if err := os.WriteFile(path, []byte("подтверждение"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Validate("акт.txt", path); err != nil {
		t.Fatal(err)
	}
}
