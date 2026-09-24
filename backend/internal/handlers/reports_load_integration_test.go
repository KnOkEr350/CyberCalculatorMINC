package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	reportrepository "cybercalc/internal/modules/reporting/repository"
	"cybercalc/internal/testfixtures"
	"cybercalc/internal/xlsx"
)

// QA-10: формирование отчётов по HTTP на согласованном объёме арендатора —
// 10 000 утверждённых мероприятий (по 5 000 плана и факта) при 10 000 чужих.
// Бюджет и допущение по объёму записаны в docs/PERFORMANCE.md; чужие строки
// обязаны отсекаться запросом, а не после выборки.
const (
	reportLoadOwnEntries     = 10_000
	reportLoadForeignEntries = 10_000
	reportBudget             = 5 * time.Second
	reportAllocBudget        = 1 << 30 // суммарные аллокации за один отчёт, байт
)

// MVP: согласование остаётся частью процесса, но не должно блокировать
// рабочую выгрузку. Черновые данные обязаны попасть в файл с честной меткой.
func TestDraftReportCanBeExported(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
		audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('teachers',$1,$2,$3,'fact',$4,'vuz','{}',100000,100000,'average',$5)`,
		tenant.partner, tenant.agreement, tenant.company, year, tenant.admin); err != nil {
		t.Fatal(err)
	}

	handler := ReportHandlers{DB: db}
	user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	recorder := httptest.NewRecorder()
	handler.Export(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/reports/export?period_type=fact&report_year=%d", year), nil), user)
	if recorder.Code != 200 {
		t.Fatalf("черновой отчёт вернул %d: %s", recorder.Code, recorder.Body.String())
	}
	rows, err := xlsx.ReadFirst(recorder.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || len(rows[1]) < 13 || rows[1][12] != "Черновик (не допущено к зачёту)" {
		t.Fatalf("черновой статус не отражён в выгрузке: %v", rows)
	}
}

func TestReportGenerationOnAgreedVolume(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)

	seed := func(tenant tenantScenario, count int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			SELECT (ARRAY['teachers','ood_rpd','internship'])[1+(g%3)],$1,$2,$3,CASE WHEN g%2=0 THEN 'plan' ELSE 'fact' END,$4,'vuz',
				'{"academic_hours":64}',100000+g,100000+g,'average',$5
			FROM generate_series(1,$6) g`,
			tenant.partner, tenant.agreement, tenant.company, year, tenant.admin, count); err != nil {
			t.Fatal(err)
		}
		// Отчёт утверждается после внесения строк: триггеры сбрасывают статус.
		for _, period := range []string{"plan", "fact"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO agreement_reports(agreement_id,report_year,period_type,status,
				scope_confirmed,conditions_confirmed,evidence_confirmed,counterparty_confirmed,approved_by,approved_at)
				VALUES($1,$2,$3,'approved',true,true,true,true,$4,now())`, tenant.agreement, year, period, tenant.admin); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.ExecContext(ctx, `ANALYZE entries`); err != nil {
			t.Fatal(err)
		}
	}
	seed(own, reportLoadOwnEntries)
	seed(foreign, reportLoadForeignEntries)
	// Приложение № 4 строится по снимку на 1 мая: снимок содержит весь факт
	// арендатора (5 000 строк), как настоящий.
	frozen := snapshotPayload{SchemaVersion: 1, CompanyID: own.company, ReportYear: year, SnapshotDate: fmt.Sprintf("%d-05-01", year), Documents: []snapshotDocument{}}
	partner, agreement := own.partner, own.agreement
	for g := 1; g <= reportLoadOwnEntries; g += 2 {
		frozen.Entries = append(frozen.Entries, snapshotEntry{ID: fmt.Sprint(g), CategoryCode: "teachers", PartnerID: &partner, AgreementID: &agreement,
			PeriodType: "fact", Audience: "vuz", Payload: json.RawMessage(`{"academic_hours":64}`), AmountRub: fmt.Sprintf("%d.00", 100000+g),
			FormulaAmount: fmt.Sprintf("%d.00", 100000+g), CostMethod: "average", Eligible: true})
	}
	frozenBytes, err := json.Marshal(frozen)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(frozenBytes)
	if _, err := db.ExecContext(ctx, `INSERT INTO report_snapshots(it_company_id,report_year,snapshot_date,payload,payload_bytes,payload_sha256,sealed_by)
		VALUES($1,$2,make_date($2,5,1),$3::jsonb,$4,$5,$6)`, own.company, year, string(frozenBytes), frozenBytes, hex.EncodeToString(digest[:]), own.admin); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM agreement_reports WHERE agreement_id IN ($1::uuid,$2::uuid)`, own.agreement, foreign.agreement)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM entries WHERE it_company_id IN ($1::uuid,$2::uuid)`, own.company, foreign.company)
	})

	handler := ReportHandlers{DB: db, Projection: reportrepository.NewActivityProjection(db)}
	user := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}

	budget := reportBudget
	if raceEnabled {
		budget *= raceSlowdown
	}
	for _, tc := range []struct {
		name, query string
		wantRows    int // строк первого листа: данные и итог; 0 — не проверять
	}{
		{"выгрузка факта по мероприятиям", "period_type=fact", reportLoadOwnEntries/2 + 2},
		{"конструктор план-факт-дельта", "report_type=plan_fact&format=xlsx", 0},
		{"Приложение № 4", "report_type=annex4", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			recorder := httptest.NewRecorder()
			started := time.Now()
			handler.Export(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/reports/export?%s&report_year=%d", tc.query, year), nil), user)
			elapsed := time.Since(started)
			runtime.ReadMemStats(&after)
			if recorder.Code != 200 {
				t.Fatalf("отчёт вернул %d: %s", recorder.Code, recorder.Body.String())
			}
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("%s: %v, %d МиБ аллокаций, файл %d КиБ", tc.name, elapsed, allocated>>20, recorder.Body.Len()>>10)
			if elapsed > budget {
				t.Fatalf("отчёт формировался %v при бюджете %v", elapsed, budget)
			}
			if allocated > reportAllocBudget*uint64(map[bool]int{false: 1, true: 4}[raceEnabled]) {
				t.Fatalf("отчёт выделил %d МиБ при бюджете %d МиБ", allocated>>20, reportAllocBudget>>20)
			}
			if tc.wantRows > 0 {
				rows, err := xlsx.ReadFirst(recorder.Body.Bytes())
				if err != nil {
					t.Fatalf("файл отчёта не читается: %v", err)
				}
				// Строки арендатора видны все, чужие — ни одной.
				if len(rows) != tc.wantRows {
					t.Fatalf("на первом листе %d строк, ожидалось %d (заголовок, данные и итог)", len(rows), tc.wantRows+1)
				}
			}
		})
	}
}
