package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/lib/pq"
)

// AUDIT-01 / SEC-02: рабочая учётная запись приложения отделена от владельца
// схемы. Права выдаются по политике, а не списком таблиц: список приходилось
// пополнять вручную при каждой миграции, и добавленная таблица оставалась без
// прав — функция молча ломалась уже в рабочем контуре.
//
// appendOnlyTables — журналы: приложение их дополняет, но не переписывает.
// Удаление устаревших записей идёт через SECURITY DEFINER-функцию очистки,
// которая выполняется от владельца схемы.
var appendOnlyTables = map[string]bool{
	"audit_log":                          true,
	"audit_log_archive":                  true,
	"organization_budget_target_history": true,
	"agreement_report_history":           true,
	"normative_sources":                  true,
	"normative_revision_diffs":           true,
}

// readOnlyTables — справочники и служебные таблицы, которые наполняет
// миграционный контейнер или доверенный процесс, а приложение только читает.
var readOnlyTables = map[string]bool{
	"schema_migrations":       true,
	"normative_trusted_hosts": true,
	"activity_categories":     true,
}

// ProvisionRuntime is invoked only by the short-lived migration container.
// Database owner credentials never enter the HTTP-serving containers.
func ProvisionRuntime(db *sql.DB, user, password string) error {
	if user == "" || password == "" {
		return fmt.Errorf("runtime DB credentials required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(270007)`); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, user).Scan(&exists); err != nil {
		return err
	}
	name := pq.QuoteIdentifier(user)
	if exists {
		var privileged bool
		if err := tx.QueryRowContext(ctx, `SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=$1`, user).Scan(&privileged); err != nil {
			return err
		}
		if privileged {
			return fmt.Errorf("runtime role must not be a database administrator")
		}
		if _, err := tx.ExecContext(ctx, "ALTER ROLE "+name+" LOGIN PASSWORD "+pq.QuoteLiteral(password)); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx, "CREATE ROLE "+name+" LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD "+pq.QuoteLiteral(password)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "GRANT USAGE ON SCHEMA public TO "+name); err != nil {
		return err
	}

	// Прежние права снимаются целиком: иначе таблица, переведённая в режим
	// «только дополнение», сохранила бы выданное когда-то право на UPDATE.
	if _, err := tx.ExecContext(ctx, "REVOKE ALL ON ALL TABLES IN SCHEMA public FROM "+name); err != nil {
		return err
	}

	tables, err := runtimeTables(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range tables {
		privileges := "SELECT,INSERT,UPDATE,DELETE"
		switch {
		case readOnlyTables[table]:
			privileges = "SELECT"
		case appendOnlyTables[table]:
			privileges = "SELECT,INSERT"
		}
		if _, err := tx.ExecContext(ctx, "GRANT "+privileges+" ON TABLE "+pq.QuoteIdentifier(table)+" TO "+name); err != nil {
			return err
		}
	}

	// Представления доступны только на чтение: писать в них приложение не должно.
	views, err := runtimeViews(ctx, tx)
	if err != nil {
		return err
	}
	for _, view := range views {
		if _, err := tx.ExecContext(ctx, "GRANT SELECT ON "+pq.QuoteIdentifier(view)+" TO "+name); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO "+name); err != nil {
		return err
	}
	// Удаление устаревших записей журнала возможно только через эту функцию.
	if _, err := tx.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION purge_expired_audit() TO "+name); err != nil {
		return err
	}
	return tx.Commit()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func runtimeTables(ctx context.Context, db queryer) ([]string, error) {
	return catalogNames(ctx, db, `SELECT tablename FROM pg_tables WHERE schemaname='public'`)
}

func runtimeViews(ctx context.Context, db queryer) ([]string, error) {
	return catalogNames(ctx, db, `SELECT viewname FROM pg_views WHERE schemaname='public'`)
}

func catalogNames(ctx context.Context, db queryer, query string) ([]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}
