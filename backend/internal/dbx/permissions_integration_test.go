package dbx

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

func permissionsDB(t *testing.T) *sql.DB {
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
	if err := RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	return db
}

// AUDIT-01 на реальной БД: рабочая учётная запись приложения не может изменить
// или удалить запись журнала. Запрет держится не только триггером в схеме, но
// и правами: даже ошибка в коде приложения не перепишет историю.
func TestRuntimeRoleCannotRewriteAuditLog(t *testing.T) {
	db := permissionsDB(t)
	ctx := context.Background()
	role := "runtime_probe_audit"
	if err := ProvisionRuntime(db, role, "probe-password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.ExecContext(ctx, `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON SCHEMA public FROM `+role)
		db.ExecContext(ctx, `DROP ROLE IF EXISTS `+role)
	})

	privilege := func(table, action string) bool {
		t.Helper()
		var granted bool
		if err := db.QueryRowContext(ctx,
			`SELECT has_table_privilege($1,$2,$3)`, role, table, action).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		return granted
	}

	// Журналы: дополнять можно, переписывать и удалять — нет.
	for _, table := range []string{"audit_log", "audit_log_archive", "organization_budget_target_history"} {
		if !privilege(table, "SELECT") || !privilege(table, "INSERT") {
			t.Errorf("%s: приложение должно читать и дополнять журнал", table)
		}
		for _, action := range []string{"UPDATE", "DELETE"} {
			if privilege(table, action) {
				t.Errorf("%s: рабочая роль не должна иметь права %s", table, action)
			}
		}
	}

	// Рабочие таблицы остаются доступными: политика не должна ломать работу.
	for _, table := range []string{"entries", "attachments", "report_snapshots", "generated_reports",
		"staff_members", "teaching_payouts", "user_partner_assignments", "sessions"} {
		for _, action := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			if !privilege(table, action) {
				t.Errorf("%s: приложению нужно право %s", table, action)
			}
		}
	}

	// Справочники, которые наполняет миграционный контейнер, — только чтение.
	for _, table := range []string{"schema_migrations", "activity_categories"} {
		if !privilege(table, "SELECT") {
			t.Errorf("%s: приложение должно читать справочник", table)
		}
		if privilege(table, "UPDATE") || privilege(table, "DELETE") {
			t.Errorf("%s: приложение не должно менять справочник", table)
		}
	}
}

// Ни одна таблица схемы не должна остаться без решения о правах: раньше список
// вёлся руками, и каждая новая миграция могла тихо оставить таблицу
// недоступной для приложения.
func TestEveryTableGetsAPrivilegeDecision(t *testing.T) {
	db := permissionsDB(t)
	ctx := context.Background()
	role := "runtime_probe_coverage"
	if err := ProvisionRuntime(db, role, "probe-password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.ExecContext(ctx, `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON SCHEMA public FROM `+role)
		db.ExecContext(ctx, `DROP ROLE IF EXISTS `+role)
	})

	tables, err := runtimeTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatal("в схеме не найдено таблиц")
	}
	for _, table := range tables {
		var granted bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,'SELECT')`, role, table).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		if !granted {
			t.Errorf("таблица %s осталась недоступной приложению", table)
		}
	}
	// Представления тоже доступны: дашборд и отчётность читают их.
	views, err := runtimeViews(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		var granted bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,'SELECT')`, role, view).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		if !granted {
			t.Errorf("представление %s осталось недоступным приложению", view)
		}
	}
}

// Повторная выдача прав идемпотентна и снимает лишнее: таблица, переведённая в
// режим «только дополнение», теряет ранее выданное право на изменение.
func TestProvisionRuntimeIsIdempotentAndRevokesStalePrivileges(t *testing.T) {
	db := permissionsDB(t)
	ctx := context.Background()
	role := "runtime_probe_repeat"
	if err := ProvisionRuntime(db, role, "probe-password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.ExecContext(ctx, `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON SCHEMA public FROM `+role)
		db.ExecContext(ctx, `DROP ROLE IF EXISTS `+role)
	})

	// Выдаём лишнее право вручную — так выглядит наследие прошлых версий.
	if _, err := db.ExecContext(ctx, `GRANT UPDATE ON audit_log TO `+role); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionRuntime(db, role, "probe-password"); err != nil {
		t.Fatal(err)
	}
	var stale bool
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'audit_log','UPDATE')`, role).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Fatal("повторная выдача прав должна снимать лишнее право на изменение журнала")
	}
}

// Рабочая роль не может быть администратором базы: иначе разделение прав
// теряет смысл.
func TestProvisionRuntimeRejectsPrivilegedRole(t *testing.T) {
	db := permissionsDB(t)
	ctx := context.Background()
	role := "runtime_probe_privileged"
	if _, err := db.ExecContext(ctx, `CREATE ROLE `+role+` LOGIN CREATEDB PASSWORD 'probe'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.ExecContext(ctx, `DROP ROLE IF EXISTS `+role) })

	if err := ProvisionRuntime(db, role, "probe-password"); err == nil {
		t.Fatal("роль с административными правами не должна использоваться как рабочая")
	}
}

// Первая запись в пустой журнал — отдельный путь: триггер цепочки ищет конец
// цепи в архиве, и без права на чтение архива свежая установка падала на самой
// первой операции, оставляя стек неработоспособным.
func TestRuntimeRoleCanWriteFirstAuditRecord(t *testing.T) {
	db := permissionsDB(t)
	ctx := context.Background()
	role := "runtime_probe_first_write"
	if err := ProvisionRuntime(db, role, "probe-password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.ExecContext(ctx, `REVOKE ALL ON ALL TABLES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM `+role)
		db.ExecContext(ctx, `REVOKE ALL ON SCHEMA public FROM `+role)
		db.ExecContext(ctx, `DROP ROLE IF EXISTS `+role)
	})

	// Ровно тот запрос, который триггер выполняет, когда журнал ещё пуст.
	for _, table := range []string{"audit_log", "audit_log_archive"} {
		var granted bool
		if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,$2,'SELECT')`, role, table).Scan(&granted); err != nil {
			t.Fatal(err)
		}
		if !granted {
			t.Fatalf("без чтения %s первая запись в журнал невозможна", table)
		}
	}
	var writable bool
	if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'audit_log','INSERT')`, role).Scan(&writable); err != nil {
		t.Fatal(err)
	}
	if !writable {
		t.Fatal("приложение должно дополнять журнал")
	}
}
