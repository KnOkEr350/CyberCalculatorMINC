package dbx

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Dedicated disposable database only: checks upgrade from the old schema,
// including the separately merged ownership migration, without losing records.
func TestWorkspaceMigrationPreservesLegacyData(t *testing.T) {
	dsn := os.Getenv("TEST_MIGRATION_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_MIGRATION_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_migration_test") {
		t.Fatal("requires disposable workspace_migration_test DB")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := os.Getenv("TEST_MIGRATIONS_DIR")
	if dir == "" {
		t.Fatal("TEST_MIGRATIONS_DIR required")
	}
	old := t.TempDir()
	for _, name := range []string{"0001_init.sql", "0002_budget_targets.sql", "0003_order_activities.sql"} {
		data, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(old, name), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := RunMigrations(db, old); e != nil {
		t.Fatal(e)
	}
	var user, partner, entry string
	if e := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,role) VALUES('legacy@migration.test','test-only','Старый администратор','admin') RETURNING id`).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO partners(name,partner_kind) VALUES('Старый вуз','vuz') RETURNING id`).Scan(&partner); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO entries(category_code,partner_id,period_type,report_year,audience,payload,amount_rub,created_by) VALUES('internship',$1,'fact',2026,'vuz','{"mentor_full_name":"Иванов Иван Иванович"}',30340,$2) RETURNING id`, partner, user).Scan(&entry); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO budget_targets(report_year,target_amount_rub,updated_by) VALUES(2026,12345,$1)`, user); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at) VALUES($1,'legacy.txt','/test-only/legacy.txt','text/plain',10,$2,now()+interval '1 year')`, entry, user); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db, dir); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db, dir); e != nil {
		t.Fatal("second migration run must be harmless:", e)
	}
	var mentor string
	var amount float64
	if e := db.QueryRow(`SELECT payload->>'mentor_id',amount_rub FROM entries WHERE id=$1`, entry).Scan(&mentor, &amount); e != nil {
		t.Fatal(e)
	}
	if mentor == "" || amount != 30340 {
		t.Fatal("legacy entry lost or recalculated")
	}
	var name, mentorPartner string
	if e := db.QueryRow(`SELECT full_name,partner_id FROM mentors WHERE id=$1`, mentor).Scan(&name, &mentorPartner); e != nil {
		t.Fatal(e)
	}
	if name != "Иванов Иван Иванович" || mentorPartner != partner {
		t.Fatal("mentor backfill broken")
	}
	var count int
	if e := db.QueryRow(`SELECT count(*) FROM attachments WHERE entry_id=$1`, entry).Scan(&count); e != nil || count != 1 {
		t.Fatal("attachment metadata lost", e)
	}
	if e := db.QueryRow(`SELECT target_amount_rub FROM budget_targets WHERE report_year=2026 AND owner_user_id=$1`, user).Scan(&amount); e != nil || amount != 12345 {
		t.Fatal("legacy budget target lost", e)
	}
	if e := db.QueryRow(`SELECT count(*) FROM education_directory`).Scan(&count); e != nil || count != 1257 {
		t.Fatalf("expected 1257 source records, got %d: %v", count, e)
	}
	t.Log("legacy entries, amounts, attachments and budget retained; mentors backfilled; 1257 directory records; migration rerun safe")
}
