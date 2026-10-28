package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// «Создать факт из плана»: значения копируются, повтор не плодит дубли,
// запись факта можно править, чужой арендатор плана не видит.
func TestCopyPlanToFact(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)
	userOf := func(tn tenantScenario) middleware.AuthUser {
		return middleware.AuthUser{ID: tn.admin, Role: models.RoleSuperAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tn.company}
	}
	handlers := EntryHandlers{DB: db}
	send := func(tn tenantScenario, serve func(http.ResponseWriter, *http.Request, middleware.AuthUser), path string, body interface{}) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		request := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		serve(recorder, request, userOf(tn))
		return recorder
	}
	createPlan := func(program string) string {
		t.Helper()
		recorder := send(tenant, handlers.Create, "/api/entries", map[string]interface{}{
			"partner_id": tenant.partner, "agreement_id": tenant.agreement, "category_code": "ood_rpd",
			"period_type": "plan", "report_year": year, "audience": "vuz",
			"payload": map[string]interface{}{"org_name": tenant.partner, "doc_type": "rpd", "level": "vo",
				"activity_type": "expertise", "program_name": program},
		})
		if recorder.Code != 201 {
			t.Fatalf("план не создан: %d %s", recorder.Code, recorder.Body.String())
		}
		var out struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(recorder.Body.Bytes(), &out)
		return out.ID
	}
	planA, planB := createPlan("Программа А"), createPlan("Программа Б")
	body := map[string]interface{}{"agreement_id": tenant.agreement, "report_year": year}

	type result struct {
		CreatedCount int `json:"created_count"`
		SkippedCount int `json:"skipped_count"`
		FailedCount  int `json:"failed_count"`
		Created      []struct {
			PlanEntryID string `json:"plan_entry_id"`
			FactEntryID string `json:"fact_entry_id"`
		} `json:"created"`
	}
	run := func(tn tenantScenario, body interface{}) (*httptest.ResponseRecorder, result) {
		t.Helper()
		recorder := send(tn, handlers.CopyPlanToFact, "/api/entries/copy-plan-to-fact", body)
		var out result
		_ = json.Unmarshal(recorder.Body.Bytes(), &out)
		return recorder, out
	}

	recorder, first := run(tenant, body)
	if recorder.Code != 200 || first.CreatedCount != 2 || first.SkippedCount != 0 || first.FailedCount != 0 {
		t.Fatalf("первое копирование: %d %s", recorder.Code, recorder.Body.String())
	}
	var facts int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE period_type='fact' AND agreement_id=$1 AND report_year=$2
		AND copied_from_entry_id IN ($3,$4) AND payload->>'program_name' IN ('Программа А','Программа Б')`,
		tenant.agreement, year, planA, planB).Scan(&facts); err != nil || facts != 2 {
		t.Fatalf("в факте должно быть 2 связанные записи, получено %d (%v)", facts, err)
	}

	// Правим факт и копируем повторно: дублей быть не должно.
	if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=jsonb_set(payload,'{program_name}','"Исправлено в факте"')
		WHERE copied_from_entry_id=$1`, planA); err != nil {
		t.Fatal(err)
	}
	recorder, second := run(tenant, body)
	if recorder.Code != 200 || second.CreatedCount != 0 || second.SkippedCount != 2 {
		t.Fatalf("повторное копирование должно пропускать записи: %d %s", recorder.Code, recorder.Body.String())
	}

	// Чужая ИТ-компания ничего не копирует из этого плана.
	if recorder, _ = run(foreign, body); recorder.Code != 409 {
		t.Fatalf("чужой арендатор: ожидался 409, получено %d %s", recorder.Code, recorder.Body.String())
	}
	// Фильтр по виду без записей — отказ, а не пустой успех.
	if recorder, _ = run(tenant, map[string]interface{}{"agreement_id": tenant.agreement, "report_year": year, "category_code": "teachers"}); recorder.Code != 409 {
		t.Fatalf("пустая выборка: ожидался 409, получено %d", recorder.Code)
	}
	if recorder, _ = run(tenant, map[string]interface{}{"report_year": year}); recorder.Code != 400 {
		t.Fatalf("без соглашения: ожидался 400, получено %d", recorder.Code)
	}
}
