package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// DATA-11 на реальной БД: норматив 3% неотделим от своего основания. База
// берётся за год N-2, подтверждение ФНС — это дата и документ вместе, а каждая
// редакция остаётся в истории.
func TestBudgetTargetKeepsBasisAndHistory(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	handlers := DashboardHandlers{DB: db}
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	year := time.Now().Year()
	notified := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	set := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("POST", "/api/dashboard/target", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handlers.SetBudgetTarget(recorder, request, user)
		return recorder
	}
	// База 10 000 000,00 ₽ — норматив ровно 300 000,00 ₽.
	const base, target = "10000000.00", "300000.00"

	t.Run("год базы сверяется с отчётным", func(t *testing.T) {
		wrong := set(fmt.Sprintf(`{"report_year":%d,"basis_year":%d,"target_amount_rub":"%s",
			"savings_base_rub":"%s","source_reference":"письмо Минцифры","notified_at":"%s"}`,
			year, year-1, target, base, notified))
		if wrong.Code != 400 || !strings.Contains(wrong.Body.String(), "N-2") {
			t.Fatalf("база за чужой год должна отклоняться: %d %s", wrong.Code, wrong.Body.String())
		}
	})

	t.Run("подтверждение ФНС — дата и документ вместе", func(t *testing.T) {
		half := set(fmt.Sprintf(`{"report_year":%d,"target_amount_rub":"%s","savings_base_rub":"%s",
			"source_reference":"письмо Минцифры","notified_at":"%s","fns_confirmed_at":"%s"}`,
			year, target, base, notified, notified))
		if half.Code != 400 {
			t.Fatalf("дата без реквизита не подтверждает базу: %d %s", half.Code, half.Body.String())
		}
		early := set(fmt.Sprintf(`{"report_year":%d,"target_amount_rub":"%s","savings_base_rub":"%s",
			"source_reference":"письмо Минцифры","notified_at":"%s","fns_confirmed_at":"%s","fns_reference":"справка ФНС № 1"}`,
			year, target, base, notified, time.Now().AddDate(0, 0, -60).Format("2006-01-02")))
		if early.Code != 400 {
			t.Fatalf("ФНС не подтверждает базу раньше её доведения: %d %s", early.Code, early.Body.String())
		}
	})

	t.Run("норматив сохраняется вместе с основанием", func(t *testing.T) {
		ok := set(fmt.Sprintf(`{"report_year":%d,"target_amount_rub":"%s","savings_base_rub":"%s",
			"source_reference":"письмо Минцифры от 01.02","notified_at":"%s"}`,
			year, target, base, notified))
		if ok.Code != 200 {
			t.Fatalf("корректный норматив должен сохраняться: %d %s", ok.Code, ok.Body.String())
		}
		var basisYear int
		if err := db.QueryRowContext(ctx, `SELECT basis_year FROM organization_budget_targets
			WHERE it_company_id::text=$1 AND report_year=$2`, tenant.company, year).Scan(&basisYear); err != nil {
			t.Fatal(err)
		}
		if basisYear != year-2 {
			t.Fatalf("год базы %d, ожидался %d", basisYear, year-2)
		}
	})

	t.Run("история хранит каждую редакцию", func(t *testing.T) {
		confirmed := set(fmt.Sprintf(`{"report_year":%d,"target_amount_rub":"%s","savings_base_rub":"%s",
			"source_reference":"письмо Минцифры от 01.02","notified_at":"%s",
			"fns_confirmed_at":"%s","fns_reference":"справка ФНС № 42"}`,
			year, target, base, notified, time.Now().Format("2006-01-02")))
		if confirmed.Code != 200 {
			t.Fatalf("подтверждение ФНС должно сохраняться: %d %s", confirmed.Code, confirmed.Body.String())
		}

		recorder := httptest.NewRecorder()
		handlers.BudgetTargetHistory(recorder,
			httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard/target-history?report_year=%d", year), nil), user)
		if recorder.Code != 200 {
			t.Fatalf("история вернула %d: %s", recorder.Code, recorder.Body.String())
		}
		var revisions []struct {
			BasisYear      *int   `json:"basis_year"`
			FNSReference   string `json:"fns_reference"`
			Operation      string `json:"operation"`
			TargetAmount   string `json:"target_amount_rub"`
			SavingsBaseRub string `json:"savings_base_rub"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &revisions); err != nil {
			t.Fatal(err)
		}
		if len(revisions) != 2 {
			t.Fatalf("в истории %d редакций, ожидались две (создание и подтверждение)", len(revisions))
		}
		// Новые записи идут первыми.
		if revisions[0].Operation != "update" || revisions[1].Operation != "insert" {
			t.Fatalf("порядок и вид операций неверны: %+v", revisions)
		}
		if revisions[0].FNSReference != "справка ФНС № 42" {
			t.Fatalf("подтверждение ФНС не попало в историю: %+v", revisions[0])
		}
		if revisions[1].FNSReference != "" {
			t.Fatal("в первой редакции подтверждения ФНС ещё не было")
		}
		for i, revision := range revisions {
			if revision.BasisYear == nil || *revision.BasisYear != year-2 {
				t.Fatalf("редакция %d без года базы: %+v", i, revision)
			}
			if revision.TargetAmount != target || revision.SavingsBaseRub != base {
				t.Fatalf("редакция %d потеряла суммы: %+v", i, revision)
			}
		}
	})

	t.Run("история только дополняется", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE organization_budget_target_history SET target_amount_rub=1`); err == nil {
			t.Fatal("историю норматива нельзя переписывать")
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM organization_budget_target_history`); err == nil {
			t.Fatal("историю норматива нельзя удалять")
		}
	})

	t.Run("история закрыта для чужого арендатора", func(t *testing.T) {
		foreign := newTenant(ctx, t, f)
		recorder := httptest.NewRecorder()
		foreignUser := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
		handlers.BudgetTargetHistory(recorder, httptest.NewRequest("GET", "/api/dashboard/target-history", nil), foreignUser)
		if recorder.Code != 200 {
			t.Fatalf("история вернула %d: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "справка ФНС № 42") {
			t.Fatal("в историю соседнего арендатора попали чужие редакции")
		}
	})
}
