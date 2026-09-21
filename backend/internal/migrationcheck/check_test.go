package migrationcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckAcceptsFrozenLegacyAndReservedActiveMigration(t *testing.T) {
	dir, manifest := fixture(t, map[string]fixtureFile{
		"0004_old.sql":   {body: "SELECT 1;", mode: "legacy"},
		"0004_other.sql": {body: "SELECT 2;", mode: "legacy"},
		"0100_flags.sql": {body: "SELECT 3;", mode: "active"},
	})
	if err := Check(dir, manifest); err != nil {
		t.Fatal(err)
	}
	owner, ok := OwnerForVersion(100)
	if !ok || owner != "platform/security" {
		t.Fatalf("unexpected owner %q, %v", owner, ok)
	}
}

func TestCheckRejectsChangedAppliedMigration(t *testing.T) {
	dir, manifest := fixture(t, map[string]fixtureFile{
		"0001_init.sql": {body: "SELECT 1;", mode: "legacy"},
	})
	if err := os.WriteFile(filepath.Join(dir, "0001_init.sql"), []byte("SELECT 2;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(dir, manifest); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("got %v, want checksum error", err)
	}
}

func TestCheckRejectsNewMigrationOutsideReservedRanges(t *testing.T) {
	dir, manifest := fixture(t, map[string]fixtureFile{
		"0023_new.sql": {body: "SELECT 1;", mode: "active"},
	})
	if err := Check(dir, manifest); err == nil || !strings.Contains(err.Error(), "0100-0999") {
		t.Fatalf("got %v, want range error", err)
	}
}

func TestCheckRejectsDuplicateActiveVersion(t *testing.T) {
	dir, manifest := fixture(t, map[string]fixtureFile{
		"0100_one.sql": {body: "SELECT 1;", mode: "active"},
		"0100_two.sql": {body: "SELECT 2;", mode: "active"},
	})
	if err := Check(dir, manifest); err == nil || !strings.Contains(err.Error(), "несколькими") {
		t.Fatalf("got %v, want duplicate version error", err)
	}
}

func TestCheckRejectsUnregisteredAndInvalidFilename(t *testing.T) {
	t.Run("unregistered", func(t *testing.T) {
		dir, manifest := fixture(t, map[string]fixtureFile{
			"0100_one.sql": {body: "SELECT 1;", mode: "active"},
		})
		if err := os.WriteFile(filepath.Join(dir, "0101_two.sql"), []byte("SELECT 2;"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := Check(dir, manifest); err == nil || !strings.Contains(err.Error(), "registry") {
			t.Fatalf("got %v, want registry error", err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		dir, manifest := fixture(t, map[string]fixtureFile{
			"100_bad-name.sql": {body: "SELECT 1;", mode: "active"},
		})
		if err := Check(dir, manifest); err == nil || !strings.Contains(err.Error(), "имя") {
			t.Fatalf("got %v, want filename error", err)
		}
	})
}

type fixtureFile struct {
	body string
	mode string
}

func fixture(t *testing.T, files map[string]fixtureFile) (string, string) {
	t.Helper()
	dir := t.TempDir()
	var lines []string
	for name, file := range files {
		content := []byte(file.body)
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		lines = append(lines, hex.EncodeToString(digest[:])+"  "+file.mode+"  "+name)
	}
	manifest := filepath.Join(t.TempDir(), "checksums.sha256")
	if err := os.WriteFile(manifest, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, manifest
}
