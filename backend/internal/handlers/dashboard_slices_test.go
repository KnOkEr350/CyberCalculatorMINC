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
	"cybercalc/internal/testfixtures"
)

func TestParseDashboardSlicesBounds(t *testing.T) {
	good := [][3]string{{"", "", ""}, {"1", "", ""}, {"13", "autumn", "green"}, {"", "spring", "yellow"}, {"", "", "red"}}
	for _, c := range good {
		if _, _, _, err := parseDashboardSlices(c[0], c[1], c[2]); err != nil {
			t.Fatalf("%v должен приниматься: %v", c, err)
		}
	}
	bad := [][3]string{{"0", "", ""}, {"14", "", ""}, {"x", "", ""}, {"", "winter", ""}, {"", "", "blue"}, {"-1", "", ""}}
	for _, c := range bad {
		if _, _, _, err := parseDashboardSlices(c[0], c[1], c[2]); err == nil {
			t.Fatalf("%v должен отклоняться", c)
		}
	}
}

// Срезы «Светофора» МЦ на реальной БД: семестр, сезон и цвет светофора
// сужают одну и ту же проекцию, а записи без семестра в семестровый срез не
// попадают.
func TestDashboardSemesterTermAndLightSlices(t *testing.T) {
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
	agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID, PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"semester":1}`, `{"semester":9}`, `{"semester":2}`, `{}`} {
		if _, err = db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,'fact',$4,'vuz',$5::jsonb,100,100,'average',$6)`,
			partner.ID, agreement.ID, company.ID, year, payload, admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	handlers := DashboardHandlers{DB: db, Projection: reportrepository.NewActivityProjection(db)}
	user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
	count := func(query string) (int, int) {
		t.Helper()
		recorder := httptest.NewRecorder()
		handlers.Get(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard?report_year=%d%s", year, query), nil), user)
		if recorder.Code != 200 {
			return 0, recorder.Code
		}
		var resp struct {
			RiskBuckets map[string]struct {
				EntryCount int `json:"entry_count"`
			} `json:"risk_buckets"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, bucket := range resp.RiskBuckets {
			total += bucket.EntryCount
		}
		return total, 200
	}
	for query, want := range map[string]int{"": 4, "&semester=1": 1, "&semester=9": 1, "&semester=5": 0, "&term=autumn": 2, "&term=spring": 1, "&semester=1&term=spring": 0} {
		if got, code := count(query); code != 200 || got != want {
			t.Fatalf("срез %q: записей %d (код %d), ожидалось %d", query, got, code, want)
		}
	}
	all, _ := count("")
	sum := 0
	for _, light := range []string{"green", "yellow", "red"} {
		got, code := count("&light=" + light)
		if code != 200 {
			t.Fatalf("светофор %s: код %d", light, code)
		}
		sum += got
	}
	if sum != all {
		t.Fatalf("три цвета светофора дают %d записей, а всего %d: срезы должны делить факт без остатка", sum, all)
	}
	for _, query := range []string{"&semester=14", "&term=winter", "&light=blue"} {
		if _, code := count(query); code != 400 {
			t.Fatalf("срез %q должен отклоняться с 400, получено %d", query, code)
		}
	}
}

// Профиль организации без назначенной ИТ-компании (только что созданный
// системный администратор) видит пустой дашборд, а не ошибку 500: область
// «unassigned» не совпадает ни с одним арендатором и не приводится к uuid.
func TestDashboardForUnassignedOrganizationProfileIsEmptyNotAnError(t *testing.T) {
	db, year := integrationDB(t)
	handlers := DashboardHandlers{DB: db, Projection: reportrepository.NewActivityProjection(db)}
	user := middleware.AuthUser{ID: "00000000-0000-0000-0000-000000000001", Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization}
	recorder := httptest.NewRecorder()
	handlers.Get(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard?report_year=%d", year), nil), user)
	if recorder.Code != 200 {
		t.Fatalf("дашборд без ИТ-компании вернул %d: %s", recorder.Code, recorder.Body.String())
	}
}
