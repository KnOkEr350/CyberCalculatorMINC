package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"cybercalc/internal/dbx"

	_ "github.com/lib/pq"
)

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	return db
}

// SEC-03 на реальной БД: именованный системный администратор создаётся на
// пустой системе и восстанавливается, если единственная учётная запись
// отключена. Без восстановления систему нельзя вернуть в работу, не правя базу
// руками.
func TestEnsureAdminCreatesAndRestoresAccess(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	email := "sec03-" + strings.ToLower(t.Name()) + "@example.test"

	// На системе уже есть действующие администраторы из других тестов, поэтому
	// работаем в откатываемой транзакции с чистым списком.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active=false WHERE role='super_admin'`); err != nil {
		t.Fatal(err)
	}

	outcome, err := EnsureAdmin(ctx, tx, Params{Email: email, PasswordHash: "hash-1"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != OutcomeCreated {
		t.Fatalf("на системе без действующих администраторов ожидалось создание, получено %q", outcome)
	}
	var role, name string
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT role,full_name,is_active FROM users WHERE lower(email)=$1`, email).
		Scan(&role, &name, &active); err != nil {
		t.Fatal(err)
	}
	if role != "super_admin" || !active {
		t.Fatalf("создана учётная запись role=%s active=%v", role, active)
	}
	if name == "" {
		t.Fatal("учётная запись должна быть именованной")
	}

	// Повторный запуск при живом администраторе ничего не меняет: пароль из
	// конфигурации не должен молча перетирать рабочий.
	again, err := EnsureAdmin(ctx, tx, Params{Email: email, PasswordHash: "hash-2"})
	if err != nil {
		t.Fatal(err)
	}
	if again != OutcomeUnchanged {
		t.Fatalf("при действующем администраторе ожидалось %q, получено %q", OutcomeUnchanged, again)
	}
	var hash string
	if err := tx.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE lower(email)=$1`, email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != "hash-1" {
		t.Fatal("пароль действующего администратора не должен переписываться при перезапуске")
	}

	// Учётную запись отключили — доступа в систему нет. Оператор задаёт новый
	// пароль в конфигурации и перезапускает службу.
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active=false WHERE lower(email)=$1`, email); err != nil {
		t.Fatal(err)
	}
	restored, err := EnsureAdmin(ctx, tx, Params{Email: email, PasswordHash: "hash-3"})
	if err != nil {
		t.Fatal(err)
	}
	if restored != OutcomeRestored {
		t.Fatalf("ожидалось восстановление доступа, получено %q", restored)
	}
	if err := tx.QueryRowContext(ctx, `SELECT password_hash,is_active FROM users WHERE lower(email)=$1`, email).
		Scan(&hash, &active); err != nil {
		t.Fatal(err)
	}
	if hash != "hash-3" || !active {
		t.Fatalf("после восстановления hash=%s active=%v", hash, active)
	}
}

// Восстановление закрывает прежние сессии: иначе чужой открытый вход пережил бы
// смену пароля.
func TestRestoreClosesExistingSessions(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	email := "sec03-sessions-" + strings.ToLower(t.Name()) + "@example.test"

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active=false WHERE role='super_admin'`); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name,role,entity_type,is_active)
		VALUES($1,'old','Отключённый Администратор','super_admin','organization',false) RETURNING id::text`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(token,token_hash,user_id,expires_at)
		VALUES($1,$1,$2,now()+interval '8 hours')`, "sec03-"+userID, userID); err != nil {
		t.Fatal(err)
	}

	outcome, err := EnsureAdmin(ctx, tx, Params{Email: email, PasswordHash: "fresh"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome != OutcomeRestored {
		t.Fatalf("ожидалось восстановление, получено %q", outcome)
	}
	var live int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id::text=$1`, userID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatalf("после восстановления осталось сессий: %d", live)
	}
}

// Пустые данные не должны приводить к созданию учётной записи без пароля.
func TestEnsureAdminRejectsEmptyParams(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	for _, params := range []Params{{Email: "", PasswordHash: "x"}, {Email: "a@example.test", PasswordHash: ""}} {
		if _, err := EnsureAdmin(ctx, db, params); err == nil {
			t.Fatalf("неполные данные не должны приниматься: %+v", params)
		}
	}
}
