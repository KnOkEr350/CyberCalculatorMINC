package dbx

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	platformaudit "cybercalc/internal/platform/audit"
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
	var user, partner, entry, auditID string
	if e := db.QueryRow(`INSERT INTO users(email,password_hash,full_name,role) VALUES('legacy@migration.test','test-only','Старый администратор','admin') RETURNING id`).Scan(&user); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO partners(name,partner_kind) VALUES('Старый вуз','vuz') RETURNING id`).Scan(&partner); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO entries(category_code,partner_id,period_type,report_year,audience,payload,amount_rub,created_by) VALUES('internship',$1,'fact',2026,'vuz','{"mentor_full_name":"Иванов Иван Иванович"}',30340,$2) RETURNING id`, partner, user).Scan(&entry); e != nil {
		t.Fatal(e)
	}
	// OOP-06: старые записи «ООП и РПД» — полная и с недостающими сведениями.
	var oopComplete, oopIncomplete string
	if e := db.QueryRow(`INSERT INTO entries(category_code,partner_id,period_type,report_year,audience,payload,amount_rub,created_by) VALUES('ood_rpd',$1,'fact',2026,'vuz','{"doc_type":"RPD","level":"vo","activity_type":"development","program_name":"Базы данных","expert_full_name":"Петров П.П."}',300000,$2) RETURNING id`, partner, user).Scan(&oopComplete); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO entries(category_code,partner_id,period_type,report_year,audience,payload,amount_rub,created_by) VALUES('ood_rpd',$1,'fact',2026,'vuz','{"program_name":"Без вида документа"}',1,$2) RETURNING id`, partner, user).Scan(&oopIncomplete); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO budget_targets(report_year,target_amount_rub,updated_by) VALUES(2026,12345,$1)`, user); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at) VALUES($1,'legacy.txt','/test-only/legacy.txt','text/plain',10,$2,now()+interval '1 year')`, entry, user); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO audit_log(entity_type,entity_id,action,user_id,new_value) VALUES('entry',$1,'create',$2,'{"legacy":true}') RETURNING id`, entry, user).Scan(&auditID); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db, dir); e != nil {
		t.Fatal(e)
	}
	if e := RunMigrations(db, dir); e != nil {
		t.Fatal("second migration run must be harmless:", e)
	}
	var docType, activity string
	var legacyFlag bool
	if e := db.QueryRow(`SELECT oop_doc_type,oop_activity,oop_incomplete FROM entries WHERE id=$1`, oopComplete).Scan(&docType, &activity, &legacyFlag); e != nil ||
		docType != "rpd" || activity != "development" || legacyFlag {
		t.Fatalf("полная старая запись ООП/РПД должна получить типизированные поля: %q %q %v %v", docType, activity, legacyFlag, e)
	}
	var findings int
	if e := db.QueryRow(`SELECT count(*) FROM legacy_backfill_findings WHERE entry_id=$1 AND rule_code='oop.model_incomplete'`, oopIncomplete).Scan(&findings); e != nil || findings != 1 {
		t.Fatalf("неполная запись ООП/РПД должна попасть в находки: %d %v", findings, e)
	}
	if e := db.QueryRow(`SELECT oop_incomplete FROM entries WHERE id=$1`, oopIncomplete).Scan(&legacyFlag); e != nil || !legacyFlag {
		t.Fatalf("неполная запись должна быть помечена: %v %v", legacyFlag, e)
	}
	var normativeHostCount int
	if e := db.QueryRow(`SELECT count(*) FROM normative_trusted_hosts`).Scan(&normativeHostCount); e != nil || normativeHostCount != 4 {
		t.Fatalf("expected four exact official normative hosts, got %d: %v", normativeHostCount, e)
	}
	var normativeSource string
	if e := db.QueryRow(`INSERT INTO normative_sources(
		act_code,title,revision,published_on,effective_on,source_url,source_host,content_sha256,
		content_type,original_filename,size_bytes,content_bytes,imported_by
	) VALUES(
		'TEST-ACT','Тестовый нормативный акт','1','2026-01-01','2026-02-01',
		'https://publication.pravo.gov.ru/document/test','publication.pravo.gov.ru',repeat('a',64),
		'text/plain','test.txt',8,convert_to('official','UTF8'),$1
	) RETURNING id`, user).Scan(&normativeSource); e != nil {
		t.Fatal("insert normative source:", e)
	}
	if _, e := db.Exec(`UPDATE normative_sources SET title='Подмена' WHERE id=$1`, normativeSource); e == nil {
		t.Fatal("normative source update must be blocked by the database")
	}
	const runtimeRole = "workspace_runtime_test"
	if e := ProvisionRuntime(db, runtimeRole, "test-only-password"); e != nil {
		t.Fatal("provision runtime role:", e)
	}
	for _, privilege := range []struct {
		table, operation string
		want             bool
	}{
		{"normative_trusted_hosts", "SELECT", true},
		{"normative_trusted_hosts", "INSERT", false},
		{"normative_sources", "SELECT", true},
		{"normative_sources", "INSERT", true},
		{"normative_sources", "UPDATE", false},
		{"normative_sources", "DELETE", false},
		{"normative_revision_diffs", "SELECT", true},
		{"normative_revision_diffs", "INSERT", true},
	} {
		var allowed bool
		if e := db.QueryRow(`SELECT has_table_privilege($1,$2,$3)`, runtimeRole, privilege.table, privilege.operation).Scan(&allowed); e != nil || allowed != privilege.want {
			t.Fatalf("runtime privilege %s on %s: got %v, want %v: %v", privilege.operation, privilege.table, allowed, privilege.want, e)
		}
	}
	for _, test := range []struct {
		kind, code string
		want       bool
	}{
		{"vuz", "09.03.01", true}, {"vuz", "38.03.05", true}, {"vuz", "45.04.04", true},
		{"vuz", "38.03.01", false}, {"vuz", "01.04.01", false}, {"vuz", "09.00.00", false},
		{"vuz", "", false}, {"school", "", true}, {"kolledj", "", true},
	} {
		var got bool
		if err := db.QueryRow(`SELECT education_matches_order($1,ARRAY[$2])`, test.kind, test.code).Scan(&got); err != nil || got != test.want {
			t.Fatalf("order filter %s %s: %v %v", test.kind, test.code, got, err)
		}
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
	var actorType, requestID string
	if e := db.QueryRow(`SELECT actor_type,request_id FROM audit_log WHERE id=$1`, auditID).Scan(&actorType, &requestID); e != nil {
		t.Fatal("legacy audit event lost:", e)
	}
	if actorType != "user" || requestID != "legacy:"+auditID {
		t.Fatalf("legacy audit event contract not backfilled: actor=%q request_id=%q", actorType, requestID)
	}
	ctx := platformaudit.WithRequestID(context.Background(), "migration-test-request")
	if e := platformaudit.Write(ctx, db, platformaudit.Event{
		Actor:  platformaudit.UserActor(user),
		Action: "update",
		Entity: platformaudit.Entity{Type: "entry", ID: entry},
		Before: map[string]any{"status": "old"},
		After:  map[string]any{"status": "new"},
	}); e != nil {
		t.Fatal("write canonical audit event:", e)
	}
	if e := db.QueryRow(`SELECT actor_type,request_id FROM audit_log WHERE request_id='migration-test-request'`).Scan(&actorType, &requestID); e != nil {
		t.Fatal("read canonical audit event:", e)
	}
	if actorType != "user" || requestID != "migration-test-request" {
		t.Fatalf("canonical audit event was not persisted: actor=%q request_id=%q", actorType, requestID)
	}
	if e := db.QueryRow(`SELECT count(*) FROM education_directory WHERE listed_in_mincifry_order_27`).Scan(&count); e != nil || count != 642 {
		t.Fatalf("expected 642 organizations from Minцифры Order 27, got %d: %v", count, e)
	}
	if e := db.QueryRow(`SELECT count(*) FROM education_directory WHERE source LIKE 'Мониторинг ВО 2025%'`).Scan(&count); e != nil || count != 1257 {
		t.Fatalf("expected all 1257 monitoring records to be preserved, got %d: %v", count, e)
	}
	var agreement, agreementStatus string
	if e := db.QueryRow(`SELECT e.agreement_id,a.status FROM entries e JOIN agreements a ON a.id=e.agreement_id WHERE e.id=$1`, entry).Scan(&agreement, &agreementStatus); e != nil {
		t.Fatal("legacy agreement backfill failed:", e)
	}
	if agreement == "" || agreementStatus != "needs_review" {
		t.Fatal("legacy agreement must be retained and marked for review")
	}
	t.Log("legacy entries, amounts, attachments and budget retained; mentors and agreements backfilled; 1257 monitoring records preserved; migration rerun safe")
}
