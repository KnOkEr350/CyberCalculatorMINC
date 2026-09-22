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
	"cybercalc/internal/testfixtures"
)

// TOP-10 / ADR-03 на реальной БД: ВУЗ A реализует ТОП-ИТ, а прочие
// обязательные виды закрыты в иной ОО B с утверждённым отчётом. Раньше SQL
// дополнительно требовал в B стажировку/практику и решение Минцифры и потому
// отказывал в законном освобождении по п. 22.
func TestClause22ExemptionOnDatabase(t *testing.T) {
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
	defer db.Close()
	if err = dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	year := time.Now().Year() // срок действия соглашений-фикстур покрывает текущий год

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

			got, err := topITAlternativeExists(ctx, db, agreementA, year, "fact")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantExemption {
				t.Fatalf("topITAlternativeExists() = %v, want %v", got, tc.wantExemption)
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
