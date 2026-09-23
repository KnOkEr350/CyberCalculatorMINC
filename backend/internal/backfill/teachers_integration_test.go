package backfill

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"

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

// TCH-08 на реальной БД: записи о нагрузке без сотрудника связываются со
// справочником, если это однозначно и ничего не ломает; остальные остаются как
// есть и помечаются на ручное уточнение.
func TestTeacherBackfillLinksWhatIsUnambiguousAndFlagsTheRest(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())

	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.AssignUserToCompany(ctx, admin.ID, company.ID); err != nil {
		t.Fatal(err)
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.CreatePartner(ctx, company, university)
	if err != nil {
		t.Fatal(err)
	}
	agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID,
		PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Справочник ОКЗ: сотруднику нужна действующая профессия. Версия
	// архивная, чтобы не конфликтовать с единственной активной.
	var okzVersion string
	if err := db.QueryRowContext(ctx, `INSERT INTO okz_catalog_versions(version,source_name,effective_on,status,imported_by)
		VALUES($1,'Тестовая версия',DATE '2020-01-01','archived',$2)
		ON CONFLICT (version) DO UPDATE SET source_name=EXCLUDED.source_name RETURNING id::text`,
		"backfill-teachers", admin.ID).Scan(&okzVersion); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"2", "25", "251", "2512"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO okz_occupations(version_id,code,name) VALUES($1::uuid,$2,'Профессия')
			ON CONFLICT DO NOTHING`, okzVersion, code); err != nil {
			t.Fatal(err)
		}
	}
	staff := func(fio string) string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO staff_members(it_company_id,fio,company_position,okz_version_id,okz_code,created_by)
			VALUES($1,$2,'Разработчик',$3::uuid,'2512',$4) RETURNING id::text`, company.ID, fio, okzVersion, admin.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ivanov := staff("Иванов Иван Иванович")
	staff("Петров Пётр")
	staff("Петров  Пётр") // тот же человек с лишним пробелом — совпадение неоднозначно

	year := time.Now().Year()
	entry := func(name string, period string, extra string) string {
		t.Helper()
		payload := `{"course_name":"Разработка ПО","semester":3,"academic_hours":64` + extra
		if name != "" {
			payload += `,"teacher_full_name":"` + name + `"`
		}
		payload += `}`
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,$4,$5,'vuz',$6::jsonb,264960,264960,'average',$7) RETURNING id::text`,
			partner.ID, agreement.ID, company.ID, period, year, payload, admin.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	linkable := entry("  иванов ИВАН   Иванович ", "plan", "")
	noName := entry("", "plan", "")
	unknown := entry("Сидоров Семён Семёнович", "plan", "")
	ambiguous := entry("Петров Пётр", "plan", "")
	// Сотрудник назван в самой записи: надёжнее, чем совпадение по ФИО.
	byPayload := entry("Другое Имя", "plan", `,"staff_member_id":"`+ivanov+`"`)
	// Отчёт по факту утверждён: связывание вернуло бы его в черновик.
	locked := entry("Иванов Иван Иванович", "fact", "")
	if _, err := db.ExecContext(ctx, `INSERT INTO agreement_reports(agreement_id,report_year,period_type,status,
		scope_confirmed,conditions_confirmed,evidence_confirmed,counterparty_confirmed,approved_by,approved_at)
		VALUES($1,$2,'fact','approved',true,true,true,true,$3,now())`, agreement.ID, year, admin.ID); err != nil {
		t.Fatal(err)
	}

	report, err := Teachers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.Linked < 2 || report.NameMissing < 1 || report.NoMatch < 1 || report.Ambiguous < 1 || report.ReportLocked < 1 {
		t.Fatalf("отчёт не отражает разбор записей: %s", report)
	}

	linkedTo := func(id string) string {
		t.Helper()
		var staffID sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT staff_member_id::text FROM entries WHERE id::text=$1`, id).Scan(&staffID); err != nil {
			t.Fatal(err)
		}
		return staffID.String
	}
	openRule := func(id string) string {
		t.Helper()
		var rule sql.NullString
		err := db.QueryRowContext(ctx, `SELECT rule_code FROM legacy_backfill_findings WHERE entry_id::text=$1 AND resolved_at IS NULL`, id).Scan(&rule)
		if err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		return rule.String
	}

	if linkedTo(linkable) != ivanov {
		t.Fatal("однозначное совпадение по ФИО (с учётом регистра и пробелов) должно связываться")
	}
	if openRule(linkable) != "" {
		t.Fatal("связанная запись не должна оставаться помеченной")
	}
	if linkedTo(byPayload) != ivanov {
		t.Fatal("сотрудник, названный в записи, должен связываться")
	}
	for id, want := range map[string]string{
		noName:    RuleTeacherNameMissing,
		unknown:   RuleTeacherNoMatch,
		ambiguous: RuleTeacherAmbiguous,
		locked:    RuleTeacherReportLocked,
	} {
		if linkedTo(id) != "" {
			t.Errorf("запись, требующая решения человека, не должна связываться автоматически (%s)", want)
		}
		if got := openRule(id); got != want {
			t.Errorf("пометка %q, ожидалась %q", got, want)
		}
	}

	// Утверждённый отчёт остался утверждённым — дозаполнение его не сбросило.
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM agreement_reports WHERE agreement_id::text=$1 AND report_year=$2 AND period_type='fact'`,
		agreement.ID, year).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Fatalf("дозаполнение сбросило утверждённый отчёт: статус %q", status)
	}
	// Для заблокированной записи сохранено предложение, которое применяет человек.
	var suggested sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT suggested_value FROM legacy_backfill_findings WHERE entry_id::text=$1 AND rule_code=$2`,
		locked, RuleTeacherReportLocked).Scan(&suggested); err != nil {
		t.Fatal(err)
	}
	if suggested.String != ivanov {
		t.Fatalf("предложение %q, ожидался найденный сотрудник", suggested.String)
	}

	// Повторный прогон ничего не меняет: пометки не размножаются.
	var before int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM legacy_backfill_findings WHERE entry_id::text=ANY($1)`,
		pgArray(noName, unknown, ambiguous, locked)).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := Teachers(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM legacy_backfill_findings WHERE entry_id::text=ANY($1)`,
		pgArray(noName, unknown, ambiguous, locked)).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != 4 || after != before {
		t.Fatalf("пометок было %d, стало %d; ожидалось по одной на запись", before, after)
	}

	// Оператор исправил запись вручную — пометка снимается при следующем прогоне.
	if _, err := db.ExecContext(ctx, `UPDATE entries SET staff_member_id=$1::uuid WHERE id::text=$2`, ivanov, unknown); err != nil {
		t.Fatal(err)
	}
	report, err = Teachers(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.Resolved < 1 {
		t.Fatalf("исправленная вручную запись должна сниматься с пометок: %s", report)
	}
	if got := openRule(unknown); got != "" {
		t.Fatalf("пометка %q осталась у исправленной записи", got)
	}
}

func pgArray(ids ...string) string { return "{" + strings.Join(ids, ",") + "}" }
