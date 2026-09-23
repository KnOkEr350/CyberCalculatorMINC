package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	reportrepository "cybercalc/internal/modules/reporting/repository"
	"cybercalc/internal/testfixtures"
)

// Согласованный объём для бюджета времени (DASH-06, QA-10): один арендатор —
// крупная ИТ-компания за отчётный год, 10 000 мероприятий (половина плановых,
// половина фактических) по разным видам, плюс 20 000 чужих мероприятий рядом:
// фильтр по арендатору обязан отсекать их до вычислений, а не после.
// Это допущение, а не измеренная продовая нагрузка: оно записано в
// docs/PERFORMANCE.md и пересматривается, когда появятся реальные объёмы.
const (
	loadOwnEntries     = 10_000
	loadForeignEntries = 20_000
	dashboardBudget    = 500 * time.Millisecond
	// Детектор гонок замедляет выполнение в разы: под ним проверяется порядок
	// величины (ловит потерю индекса, а не десятки миллисекунд), а строгий
	// бюджет — в обычном прогоне.
	raceSlowdown = 6
)

func TestDashboardLatencyOnAgreedVolume(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)

	seed := func(tenant tenantScenario, count int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			SELECT (ARRAY['teachers','ood_rpd','internship','edu_content','it_clubs'])[1+(g%5)],
				$1,$2,$3,CASE WHEN g%2=0 THEN 'plan' ELSE 'fact' END,$4,(ARRAY['vuz','kolledj','school'])[1+(g%3)],
				'{}',100000+g,100000+g,'average',$5
			FROM generate_series(1,$6) g`,
			tenant.partner, tenant.agreement, tenant.company, year, tenant.admin, count); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `ANALYZE entries`); err != nil {
			t.Fatal(err)
		}
	}
	seed(own, loadOwnEntries)
	seed(foreign, loadForeignEntries)
	// Общая база не должна разбухать: десятки тысяч строк мешали бы соседним
	// проверкам.
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM entries WHERE it_company_id IN ($1::uuid,$2::uuid)`, own.company, foreign.company)
	})

	handlers := DashboardHandlers{DB: db, Projection: reportrepository.NewActivityProjection(db)}
	user := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	url := fmt.Sprintf("/api/dashboard?report_year=%d", year)

	get := func() (time.Duration, string) {
		recorder := httptest.NewRecorder()
		started := time.Now()
		handlers.Get(recorder, httptest.NewRequest("GET", url, nil), user)
		elapsed := time.Since(started)
		if recorder.Code != 200 {
			t.Fatalf("дашборд вернул %d: %s", recorder.Code, recorder.Body.String())
		}
		return elapsed, recorder.Body.String()
	}
	get() // прогрев: планы запросов и соединения
	const runs = 7
	samples := make([]time.Duration, 0, runs)
	var body string
	for i := 0; i < runs; i++ {
		elapsed, b := get()
		samples = append(samples, elapsed)
		body = b
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	median := samples[runs/2]
	t.Logf("дашборд на %d мероприятиях: медиана %v, худший %v", loadOwnEntries, median, samples[runs-1])

	budget := dashboardBudget
	if raceEnabled {
		budget *= raceSlowdown
	}
	if median > budget {
		t.Fatalf("медиана ответа дашборда %v превышает бюджет %v на %d мероприятиях арендатора", median, budget, loadOwnEntries)
	}
	// Скорость не должна достигаться потерей данных: считаются ровно записи
	// арендатора, чужие 20 000 не попадают в итоги.
	var resp struct {
		PlanTotalRub string `json:"plan_total_rub"`
		FactTotalRub string `json:"fact_total_rub"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	// Суммы: plan — чётные g, fact — нечётные, по 100000+g.
	var wantPlan, wantFact int64
	for g := 1; g <= loadOwnEntries; g++ {
		if g%2 == 0 {
			wantPlan += int64(100000 + g)
		} else {
			wantFact += int64(100000 + g)
		}
	}
	if resp.PlanTotalRub != fmt.Sprintf("%d.00", wantPlan) || resp.FactTotalRub != fmt.Sprintf("%d.00", wantFact) {
		t.Fatalf("итоги план %s / факт %s, ожидались %d.00 / %d.00", resp.PlanTotalRub, resp.FactTotalRub, wantPlan, wantFact)
	}
}
