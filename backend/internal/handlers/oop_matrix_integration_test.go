package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/testfixtures"
)

// OOP-05 на реальной БД: матрица 2×3 (РПД/ООП × разработка/актуализация/
// экспертиза) должна сходиться с самим реестром записей — и по количеству,
// и по суммам.
func TestOopMatrixMatchesRegistry(t *testing.T) {
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

	// Реестр: несколько документов в разных клетках матрицы, включая две
	// записи в одной клетке — их количество и суммы должны сложиться.
	registry := []struct {
		docType, activity string
		amount            string
	}{
		{"oop", "development", "2039850"},
		{"rpd", "development", "300000"},
		{"rpd", "development", "300000"},
		{"rpd", "expertise", "55000"},
		{"oop", "update", "626110"},
	}
	for _, item := range registry {
		payload := fmt.Sprintf(`{"doc_type":%q,"activity_type":%q,"level":"vo","program_name":"Программа"}`, item.docType, item.activity)
		if _, err = db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('ood_rpd',$1,$2,$3,'fact',$4,'vuz',$5::jsonb,$6::numeric,$6::numeric,'average',$7)`,
			partner.ID, agreement.ID, company.ID, year, payload, item.amount, admin.ID); err != nil {
			t.Fatalf("внести запись реестра %s/%s: %v", item.docType, item.activity, err)
		}
	}

	handlers := EntryHandlers{DB: db}
	user := middleware.AuthUser{ID: admin.ID, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
	recorder := httptest.NewRecorder()
	path := fmt.Sprintf("/api/entries/summary?report_year=%d&period_type=fact&partner_id=%s", year, partner.ID)
	handlers.Summary(recorder, httptest.NewRequest("GET", path, nil), user)
	if recorder.Code != 200 {
		t.Fatalf("сводка вернула %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		TotalRub money.Amount `json:"amount_rub"`
		Matrix   []struct {
			DocumentType string       `json:"document_type"`
			ActivityType string       `json:"activity_type"`
			Count        int          `json:"count"`
			AmountRub    money.Amount `json:"amount_rub"`
		} `json:"ood_rpd_matrix"`
	}
	if err = json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%v: %s", err, recorder.Body.String())
	}

	// Ожидания считаем из самого реестра, а не из повторения формул.
	wantCount := map[string]int{}
	wantAmount := map[string]money.Amount{}
	var wantTotal money.Amount
	for _, item := range registry {
		key := item.docType + "/" + item.activity
		amount, parseErr := money.Parse(item.amount)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		wantCount[key]++
		wantAmount[key], _ = money.Add(wantAmount[key], amount)
		wantTotal, _ = money.Add(wantTotal, amount)
	}
	if len(resp.Matrix) != len(wantCount) {
		t.Fatalf("клеток матрицы %d, в реестре заполнено %d: %+v", len(resp.Matrix), len(wantCount), resp.Matrix)
	}
	var matrixTotal money.Amount
	for _, got := range resp.Matrix {
		key := got.DocumentType + "/" + got.ActivityType
		if got.Count != wantCount[key] {
			t.Fatalf("%s: в матрице %d документов, в реестре %d", key, got.Count, wantCount[key])
		}
		if got.AmountRub != wantAmount[key] {
			t.Fatalf("%s: в матрице %s ₽, в реестре %s ₽", key, got.AmountRub, wantAmount[key])
		}
		matrixTotal, _ = money.Add(matrixTotal, got.AmountRub)
	}
	// Сумма матрицы сходится с итогом сводки и с реестром.
	if matrixTotal != wantTotal || resp.TotalRub != wantTotal {
		t.Fatalf("итоги разошлись: матрица %s ₽, сводка %s ₽, реестр %s ₽", matrixTotal, resp.TotalRub, wantTotal)
	}
}
