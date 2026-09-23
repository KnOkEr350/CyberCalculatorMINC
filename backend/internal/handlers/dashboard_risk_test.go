package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	reportrepository "cybercalc/internal/modules/reporting/repository"
	"cybercalc/internal/money"
	"cybercalc/internal/testfixtures"
)

// DASH-02: корзины риска не пересекаются — каждая запись попадает ровно в
// одну, а незнакомое состояние движка не создаёт четвёртую корзину и не
// утекает в гарантированный объём.
func TestRiskBucketStateIsExclusive(t *testing.T) {
	cases := []struct {
		state    string
		approved bool
		want     string
	}{
		{"green", true, "green"},
		{"green", false, "yellow"}, // комплект собран, но отчёт не утверждён
		{"yellow", true, "yellow"},
		{"yellow", false, "yellow"},
		{"red", true, "red"},
		{"red", false, "red"},
		{"", true, "red"},        // неизвестное состояние — не гарантия
		{"unknown", true, "red"}, // новое состояние движка не должно теряться
	}
	allowed := map[string]bool{"green": true, "yellow": true, "red": true}
	for _, tc := range cases {
		got := riskBucketState(tc.state, tc.approved)
		if got != tc.want {
			t.Fatalf("riskBucketState(%q, %v) = %q, want %q", tc.state, tc.approved, got, tc.want)
		}
		if !allowed[got] {
			t.Fatalf("riskBucketState(%q, %v) вернула корзину вне green/yellow/red: %q", tc.state, tc.approved, got)
		}
	}
}

// DASH-02 на реальной БД: сумма корзин совпадает с общим фактом дашборда, а
// число записей в корзинах — с числом фактических мероприятий.
func TestDashboardRiskBucketsSumUpToFactTotal(t *testing.T) {
	db, year := integrationDB(t)
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
	// Три факта с разной комплектностью документов: они должны разойтись по
	// корзинам, но в сумме дать весь факт.
	for _, amount := range []string{"100000", "250000.50", "17.25"} {
		if _, err = db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,'fact',$4,'vuz','{}',$5::numeric,$5::numeric,'average',$6)`,
			partner.ID, agreement.ID, company.ID, year, amount, admin.ID); err != nil {
			t.Fatal(err)
		}
	}

	handlers := DashboardHandlers{DB: db, Projection: reportrepository.NewActivityProjection(db)}
	user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
	recorder := httptest.NewRecorder()
	handlers.Get(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard?report_year=%d", year), nil), user)
	if recorder.Code != 200 {
		t.Fatalf("дашборд вернул %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		FactTotalRub money.Amount `json:"fact_total_rub"`
		RiskBuckets  map[string]struct {
			AmountRub  money.Amount `json:"amount_rub"`
			EntryCount int          `json:"entry_count"`
		} `json:"risk_buckets"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.RiskBuckets) != 3 {
		t.Fatalf("ожидались ровно три корзины, получено %d: %+v", len(resp.RiskBuckets), resp.RiskBuckets)
	}
	var total money.Amount
	entries := 0
	for name, bucket := range resp.RiskBuckets {
		if name != "green" && name != "yellow" && name != "red" {
			t.Fatalf("неизвестная корзина риска %q", name)
		}
		total, _ = money.Add(total, bucket.AmountRub)
		entries += bucket.EntryCount
	}
	if total != resp.FactTotalRub {
		t.Fatalf("сумма корзин %s не сходится с фактом дашборда %s", total, resp.FactTotalRub)
	}
	if entries != 3 {
		t.Fatalf("записей в корзинах %d, внесено 3 мероприятия", entries)
	}
}
