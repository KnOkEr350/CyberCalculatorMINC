package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/lib/pq"
	"time"
)

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
	for _, table := range []string{"users", "partners", "entries", "entry_comments", "mentors", "education_directory", "budget_targets", "organization_budget_targets", "settings", "attachments", "sessions", "auth_rate_limits", "file_deletion_queue", "entry_imports", "mfa_recovery_codes"} {
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, table).Scan(&present); err != nil {
			return err
		}
		if present {
			if _, err := tx.ExecContext(ctx, "GRANT SELECT,INSERT,UPDATE,DELETE ON TABLE "+pq.QuoteIdentifier(table)+" TO "+name); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "GRANT SELECT ON activity_categories TO "+name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "GRANT SELECT ON entry_eligibility TO "+name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "GRANT SELECT,INSERT ON audit_log TO "+name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO "+name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "GRANT EXECUTE ON FUNCTION purge_expired_audit() TO "+name); err != nil {
		return err
	}
	return tx.Commit()
}
