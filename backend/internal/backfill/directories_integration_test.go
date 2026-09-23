package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"
)

func directoriesDB(t *testing.T) *sql.DB {
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

// cleanupRows убирает строки теста: общая БД используется параллельными
// пакетами, и лишние ИТ-компании ломают чужие проверки числа записей.
func cleanupRows(t *testing.T, db *sql.DB, company, admin string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.ExecContext(ctx, `DELETE FROM directory_backfill_findings WHERE it_company_id::text=$1 OR entity_id::text=$1
			OR entity_id IN (SELECT id FROM partners WHERE it_company_id::text=$1)
			OR entity_id IN (SELECT id FROM agreements WHERE it_company_id::text=$1)`, company)
		_, _ = db.ExecContext(ctx, `DELETE FROM agreements WHERE it_company_id::text=$1`, company)
		_, _ = db.ExecContext(ctx, `DELETE FROM partners WHERE it_company_id::text=$1`, company)
		_, _ = db.ExecContext(ctx, `DELETE FROM accredited_it_companies WHERE id::text=$1`, company)
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id::text=$1`, admin)
	})
}

// DATA-13 на реальной БД: перенос справочников помечает неполные и
// неоднозначные строки, не меняет их, идемпотентен и закрывает исправленное.
func TestDirectoriesBackfillFlagsWhatIsIncomplete(t *testing.T) {
	db := directoriesDB(t)
	ctx := context.Background()
	unique := fmt.Sprintf("%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())

	var company, admin string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES($1,'x','Админ','super_admin','organization') RETURNING id`, unique+"@backfill.invalid").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO accredited_it_companies(name,inn,ogrn,accreditation_number,accreditation_status,registry_record_id,registry_updated_at,source_url,created_by)
		VALUES($1,$4,'','', 'active',$3,now(),'https://example.invalid/registry',$2) RETURNING id`, "Компания "+unique, admin, "reg-"+unique, "").Scan(&company); err != nil {
		t.Fatal(err)
	}
	cleanupRows(t, db, company, admin)
	newPartner := func(name string) string {
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO partners(name,partner_kind,it_company_id) VALUES($1,'vuz',$2) RETURNING id`, name, company).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first, second := newPartner("МГУ  им. Ломоносова"), newPartner("мгу им. ломоносова")
	var agreement string
	if err := db.QueryRowContext(ctx, `INSERT INTO agreements(agreement_kind,number,status,signed_on,valid_from,valid_until,it_company_id)
		VALUES('education_organization','LEGACY-1234','needs_review',DATE '2026-01-01',DATE '2026-01-01',DATE '9999-12-31',$1) RETURNING id`, company).Scan(&agreement); err != nil {
		t.Fatal(err)
	}

	count := func(entityType, rule, entity string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings WHERE entity_type=$1 AND rule_code=$2 AND entity_id::text=$3 AND resolved_at IS NULL`,
			entityType, rule, entity).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	report, err := Directories(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(report)
	for _, want := range []struct{ entity, rule, id string }{
		{"partner", RulePartnerDuplicateName, first}, {"partner", RulePartnerDuplicateName, second},
		{"partner", RulePartnerNoDirectory, first},
		{"agreement", RuleAgreementNeedsReview, agreement}, {"agreement", RuleAgreementTechnicalNo, agreement},
		{"agreement", RuleAgreementOpenEnded, agreement},
		{"it_company", RuleITCompanyNoRequisites, company}, {"it_company", RuleITCompanyNoAccredation, company},
	} {
		if count(want.entity, want.rule, want.id) != 1 {
			t.Errorf("%s %s %s: находка должна быть открыта", want.entity, want.rule, want.id)
		}
	}

	t.Run("прогон идемпотентен и данные не меняются", func(t *testing.T) {
		var before, after int
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings`).Scan(&before)
		if _, err := Directories(ctx, db); err != nil {
			t.Fatal(err)
		}
		_ = db.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings`).Scan(&after)
		if before != after {
			t.Fatalf("повторный прогон добавил находки: %d → %d", before, after)
		}
		var number, name string
		_ = db.QueryRowContext(ctx, `SELECT a.number,p.name FROM agreements a,partners p WHERE a.id=$1 AND p.id=$2`, agreement, first).Scan(&number, &name)
		if number != "LEGACY-1234" || name != "МГУ  им. Ломоносова" {
			t.Fatalf("данные справочников прогон не меняет: %q %q", number, name)
		}
	})

	t.Run("исправленное закрывается и остаётся в истории", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE partners SET name='РУДН' WHERE id=$1`, second); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE agreements SET number='ДС-17',status='draft' WHERE id=$1`, agreement); err != nil {
			t.Fatal(err)
		}
		report, err := Directories(ctx, db)
		if err != nil || report.Resolved < 4 {
			t.Fatalf("исправленные находки закрываются: %+v %v", report, err)
		}
		if count("partner", RulePartnerDuplicateName, first) != 0 || count("agreement", RuleAgreementTechnicalNo, agreement) != 0 ||
			count("agreement", RuleAgreementNeedsReview, agreement) != 0 {
			t.Fatal("исправленное не должно оставаться открытым")
		}
		var resolved int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings WHERE entity_id=$1 AND resolved_at IS NOT NULL`, agreement).Scan(&resolved); err != nil || resolved < 2 {
			t.Fatalf("история находок сохраняется: %d %v", resolved, err)
		}
		if count("agreement", RuleAgreementOpenEnded, agreement) != 1 {
			t.Fatal("неисправленное остаётся открытым")
		}
	})
}

// INTG-04: сверка видит нарушенную целостность, а открытые находки не дают
// объявить переход готовым. Общая тестовая БД содержит строки соседних тестов
// (в том числе намеренно неполные), поэтому проверяется изменение набора
// непройденных барьеров, а не их абсолютное число.
func TestReconcileGatesCatchBrokenIntegrity(t *testing.T) {
	db := directoriesDB(t)
	ctx := context.Background()
	failing := func(r ReconcileReport) map[string]bool {
		out := map[string]bool{}
		for _, gate := range r.Gates {
			if !gate.Passed {
				out[gate.Name] = true
			}
		}
		return out
	}
	baseline, err := Reconcile(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Gates) < 8 {
		t.Fatalf("барьеров должно быть не меньше восьми: %d", len(baseline.Gates))
	}
	if failing(baseline)["agreements.have_revision"] || failing(baseline)["entries.tenant_matches_agreement"] {
		t.Fatalf("на свежей схеме эти барьеры проходят: %s", baseline)
	}

	// Нарушаем: у соглашения нет редакции истории (защиту обходим намеренно).
	unique := fmt.Sprintf("rec_%d", time.Now().UnixNano())
	inn := fmt.Sprintf("%010d", time.Now().UnixNano()%10000000000)
	var admin, company, agreement string
	if err := db.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES($1,'x','А','super_admin','organization') RETURNING id`, unique+"@r.invalid").Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO accredited_it_companies(name,inn,ogrn,accreditation_number,accreditation_status,registry_record_id,registry_updated_at,source_url,created_by)
		VALUES($1,$4,$5,'1','active',$2,now(),'https://example.invalid',$3) RETURNING id`, "К "+unique, "r"+unique, admin, inn, "1"+inn+"00").Scan(&company); err != nil {
		t.Fatal(err)
	}
	cleanupRows(t, db, company, admin)
	if err := db.QueryRowContext(ctx, `INSERT INTO agreements(agreement_kind,number,status,signed_on,valid_from,valid_until,it_company_id)
		VALUES('education_organization','ДС-1','draft',DATE '2026-01-01',DATE '2026-01-01',DATE '2026-12-31',$1) RETURNING id`, company).Scan(&agreement); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE agreement_revisions DISABLE TRIGGER agreement_revisions_append_only`); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `DELETE FROM agreement_revisions WHERE agreement_id=$1`, agreement)
	if _, e := db.ExecContext(ctx, `ALTER TABLE agreement_revisions ENABLE TRIGGER agreement_revisions_append_only`); e != nil {
		t.Fatal(e)
	}
	if err != nil {
		t.Fatal(err)
	}
	broken, err := Reconcile(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !failing(broken)["agreements.have_revision"] || broken.Consistent() || broken.ReadyForCutover() || !strings.Contains(broken.String(), "agreements.have_revision") {
		t.Fatalf("потерянная редакция истории должна ломать сверку: %s", broken)
	}
	// Восстанавливаем, чтобы не оставлять общую БД в нарушенном состоянии.
	if _, err := db.ExecContext(ctx, `SELECT agreement_record_revision($1::uuid)`, agreement); err != nil {
		t.Fatal(err)
	}
	fixed, err := Reconcile(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if failing(fixed)["agreements.have_revision"] {
		t.Fatalf("после восстановления барьер проходит: %s", fixed)
	}

	// Открытые находки не дают объявить переход готовым, даже при целостности.
	clean := ReconcileReport{Gates: []Gate{{Name: "x", Passed: true}}}
	if !clean.ReadyForCutover() {
		t.Fatal("целостность без находок — переход готов")
	}
	clean.OpenFindings = 3
	if clean.ReadyForCutover() || !clean.Consistent() {
		t.Fatal("открытые находки блокируют переход, но не целостность")
	}
}
