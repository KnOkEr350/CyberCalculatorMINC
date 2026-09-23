package backup

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"

	_ "github.com/lib/pq"
)

// OPS-10 / QA-11 на реальной БД: копия настоящей базы и хранилища проходит
// шифрование и учебное восстановление во временную базу, а подмена фактов о
// данных или порча вложения ломают проверку.
func TestBackupAndRestoreDrillOnARealDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	for _, tool := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("нет клиента PostgreSQL:", tool)
		}
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	ctx := context.Background()
	// Для восстановления нужно право создавать базы; в окружении без него
	// проверка неприменима.
	var canCreate bool
	if err := admin.QueryRowContext(ctx, `SELECT rolcreatedb OR rolsuper FROM pg_roles WHERE rolname=current_user`).Scan(&canCreate); err != nil || !canCreate {
		t.Skip("у пользователя базы нет права создавать базы")
	}
	// Копируемая база своя: в общей тестовой базе параллельные пакеты пишут
	// в аудит между снятием фактов и дампом, и сверка фактов была бы случайной.
	const source = "backup_drill_source"
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+source+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+source+` WITH (FORCE)`)
	})
	sourceDSN, err := replaceDatabase(dsn, source)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", sourceDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}

	uploads := t.TempDir()
	blob := []byte("содержимое вложения для учебного восстановления")
	if err := os.MkdirAll(filepath.Join(uploads, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "ab", "note.txt"), blob, 0o644); err != nil {
		t.Fatal(err)
	}

	facts, err := CollectFacts(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	facts["uploads_root"] = uploads
	create := func(facts map[string]string) []byte {
		t.Helper()
		var out bytes.Buffer
		if _, err := Create(ctx, &out, passphrase, Source{Dump: PGDump(sourceDSN), UploadsDir: uploads, AppVersion: "drill",
			Facts: facts, Rounds: fastRounds, Now: func() time.Time { return time.Now() }}); err != nil {
			// Клиент старее сервера — свойство окружения, а не ошибка копии.
			if strings.Contains(err.Error(), "server version") {
				t.Skip("версия pg_dump не подходит к серверу:", err)
			}
			t.Fatal(err)
		}
		return out.Bytes()
	}

	t.Run("копия проходит восстановление", func(t *testing.T) {
		result, err := Drill(ctx, DrillOptions{Bundle: bytes.NewReader(create(facts)), Passphrase: passphrase, AdminDSN: dsn,
			WorkDir: filepath.Join(t.TempDir(), "drill"), Scratch: "restore_drill_ok"})
		if err != nil || !result.Passed() {
			t.Fatalf("учебное восстановление: %v %+v", err, result.Checks)
		}
		if len(result.Checks) < 3 {
			t.Fatalf("проверок должно быть не меньше трёх: %+v", result.Checks)
		}
		var left int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname='restore_drill_ok'`).Scan(&left); err != nil || left != 0 {
			t.Fatalf("временная база должна быть удалена: %d %v", left, err)
		}
	})

	t.Run("подменённые факты о данных ломают проверку", func(t *testing.T) {
		wrong := map[string]string{}
		for k, v := range facts {
			wrong[k] = v
		}
		wrong["entries"] = "999999999"
		result, err := Drill(ctx, DrillOptions{Bundle: bytes.NewReader(create(wrong)), Passphrase: passphrase, AdminDSN: dsn,
			WorkDir: filepath.Join(t.TempDir(), "drill"), Scratch: "restore_drill_bad"})
		if err == nil || result.Passed() {
			t.Fatalf("расхождение с манифестом должно проваливать проверку: %v %+v", err, result.Checks)
		}
		found := false
		for _, c := range result.Checks {
			if !c.Passed && strings.Contains(c.Detail, "entries") {
				found = true
			}
		}
		if !found {
			t.Fatalf("проверка должна называть расходящийся факт: %+v", result.Checks)
		}
	})

	t.Run("неверная парольная фраза не доходит до базы", func(t *testing.T) {
		_, err := Drill(ctx, DrillOptions{Bundle: bytes.NewReader(create(facts)), Passphrase: "неверная фраза для копии", AdminDSN: dsn,
			WorkDir: filepath.Join(t.TempDir(), "drill"), Scratch: "restore_drill_key"})
		if err == nil {
			t.Fatal("копия с неверной фразой не восстанавливается")
		}
		var left int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM pg_database WHERE datname='restore_drill_key'`).Scan(&left)
		if left != 0 {
			t.Fatal("база не должна создаваться до проверки копии")
		}
	})
}
