package handlers

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/testfixtures"
)

// integrationDB открывает изолированную тестовую БД и накатывает миграции;
// без TEST_DATABASE_DSN тест пропускается, как и остальные интеграционные.
func integrationDB(t *testing.T) (*sql.DB, int) {
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
	if err = dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	// Срок действия соглашений-фикстур покрывает текущий год.
	return db, time.Now().Year()
}

// OOP-04 / ADR-11 на реальной БД: для соглашения с вузом Виды 1 и 3
// обязательны сами по себе. Раньше проверялись только виды, перечисленные в
// соглашении, поэтому забытый в перечне Вид 3 молча не контролировался.
func TestMandatoryHigherEducationActivitiesOnDatabase(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name         string
		college      bool
		wantRequired []string
	}{
		{"вуз: Виды 1 и 3 обязательны даже вне перечня соглашения", false, []string{"ood_rpd", "teachers"}},
		{"СПО: обязательных видов нет", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			institution, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
			if tc.college {
				institution, err = f.CreateCollege(ctx, testfixtures.EducationParams{})
			}
			if err != nil {
				t.Fatal(err)
			}
			partner, err := f.CreatePartner(ctx, company, institution)
			if err != nil {
				t.Fatal(err)
			}
			// В соглашении указан только Вид 1: Вид 3 «забыт».
			agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID,
				PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID, ActivityCodes: []string{"teachers"}})
			if err != nil {
				t.Fatal(err)
			}
			user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
			workflow, err := buildWorkflow(ctx, db, user, agreement.ID, year, "fact")
			if err != nil {
				t.Fatal(err)
			}
			if workflow.HigherEducation == tc.college {
				t.Fatalf("higher_education = %v для %s", workflow.HigherEducation, institution.Kind)
			}
			checked := map[string]bool{}
			for _, activity := range workflow.Activities {
				checked[activity.Code] = true
			}
			if tc.college {
				if checked["ood_rpd"] {
					t.Fatalf("для СПО Вид 3 не должен становиться обязательным: %+v", workflow.Activities)
				}
				return
			}
			for _, code := range tc.wantRequired {
				if !checked[code] {
					t.Fatalf("для вуза вид %s должен проверяться даже вне перечня соглашения: %+v", code, workflow.Activities)
				}
			}
		})
	}
}

// TOP-10 / ADR-03 на реальной БД: ВУЗ A реализует ТОП-ИТ, а прочие
// обязательные виды закрыты в иной ОО B с утверждённым отчётом. Раньше SQL
// дополнительно требовал в B стажировку/практику и решение Минцифры и потому
// отказывал в законном освобождении по п. 22.
func TestClause22ExemptionOnDatabase(t *testing.T) {
	db, year := integrationDB(t)

	for _, tc := range []struct {
		name          string
		otherActivity []string
		wantExemption bool
	}{
		{"в иной ОО реализованы только обязательные Виды 1 и 3", []string{"teachers", "ood_rpd"}, true},
		{"в иной ОО реализован только Вид 1", []string{"teachers"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			newUniversity := func(activities []string) (partnerID, agreementID string) {
				t.Helper()
				university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
				if err != nil {
					t.Fatal(err)
				}
				partner, err := f.CreatePartner(ctx, company, university)
				if err != nil {
					t.Fatal(err)
				}
				agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID,
					PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID, ActivityCodes: activities})
				if err != nil {
					t.Fatal(err)
				}
				return partner.ID, agreement.ID
			}
			addFact := func(partnerID, agreementID, category string) {
				t.Helper()
				if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
					audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
					VALUES($1,$2,$3,$4,'fact',$5,'vuz','{}',100000,100000,'average',$6)`,
					category, partnerID, agreementID, company.ID, year, admin.ID); err != nil {
					t.Fatalf("insert %s entry: %v", category, err)
				}
			}

			partnerA, agreementA := newUniversity([]string{"top_it", "teachers", "ood_rpd"})
			addFact(partnerA, agreementA, "top_it")

			partnerB, agreementB := newUniversity([]string{"teachers", "ood_rpd"})
			for _, category := range tc.otherActivity {
				addFact(partnerB, agreementB, category)
			}
			// Отчёт B утверждается после внесения мероприятий: триггеры
			// сбрасывают статус при любом изменении entries.
			if _, err = db.ExecContext(ctx, `INSERT INTO agreement_reports(agreement_id,report_year,period_type,status,approved_by,approved_at)
				VALUES($1,$2,'fact','approved',$3,now())`, agreementB, year, admin.ID); err != nil {
				t.Fatal(err)
			}

			basis, err := topITAlternativeBasis(ctx, db, agreementA, year, "fact")
			if err != nil {
				t.Fatal(err)
			}
			if (basis != nil) != tc.wantExemption {
				t.Fatalf("topITAlternativeBasis() = %+v, освобождение ожидалось: %v", basis, tc.wantExemption)
			}
			if basis != nil {
				// ADR-03: освобождение должно быть проверяемым — видно, какая
				// ОО и какие именно записи Видов 1 и 3 его подтверждают.
				if basis.AgreementID != agreementB {
					t.Fatalf("основание ссылается на соглашение %s, ожидалось %s", basis.AgreementID, agreementB)
				}
				if basis.PartnerName == "" || basis.AgreementNumber == "" {
					t.Fatalf("основание без наименования ОО или номера соглашения: %+v", basis)
				}
				gotCategories := map[string]money.Amount{}
				for _, record := range basis.Records {
					if record.EntryCount != 1 {
						t.Fatalf("%s: записей %d, ожидалась 1", record.CategoryCode, record.EntryCount)
					}
					if record.CategoryName == "" {
						t.Fatalf("%s: пустое название вида мероприятия", record.CategoryCode)
					}
					gotCategories[record.CategoryCode] = record.AmountRub
				}
				for _, category := range []string{"teachers", "ood_rpd"} {
					if amount, ok := gotCategories[category]; !ok || amount != money.Amount(10000000) {
						t.Fatalf("в основании нет записи %s на 100 000 ₽: %+v", category, basis.Records)
					}
				}
			}
			user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
			workflow, err := buildWorkflow(ctx, db, user, agreementA, year, "fact")
			if err != nil {
				t.Fatal(err)
			}
			if workflow.TopITException != tc.wantExemption {
				t.Fatalf("buildWorkflow().TopITException = %v, want %v", workflow.TopITException, tc.wantExemption)
			}
		})
	}
}
