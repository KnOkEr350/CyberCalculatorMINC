package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// tenantScenario — изолированный арендатор: ИТ-компания, её вуз, партнёрская
// связь, соглашение и администратор компании.
type tenantScenario struct {
	admin, company, partner, agreement string
}

func newTenant(ctx context.Context, t *testing.T, f *testfixtures.Factory) tenantScenario {
	t.Helper()
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
	return tenantScenario{admin: admin.ID, company: company.ID, partner: partner.ID, agreement: agreement.ID}
}

// INT-03 на реальной БД: наставник берётся из справочника своей ОО. Ссылка на
// наставника другого партнёра — потерянная связь, и она не должна приниматься
// ни по идентификатору, ни по совпадению ФИО. Имя в отчёт берётся из
// справочника, а не из запроса.
func TestMentorMustBelongToPartner(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)

	const sharedName = "Васильев Максим Андреевич"
	newMentor := func(partner, name string) string {
		t.Helper()
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO mentors(partner_id,full_name) VALUES($1,$2) RETURNING id::text`, partner, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ownMentor := newMentor(own.partner, sharedName)
	foreignMentor := newMentor(foreign.partner, "Григорьев Пётр Сергеевич")
	// Полный тёзка у другого партнёра: совпадение ФИО не даёт доступа.
	newMentor(foreign.partner, sharedName)

	handlers := EntryHandlers{DB: db}
	request := httptest.NewRequest("GET", "/api/entries", nil)
	for _, category := range []string{"internship", "employment_practice"} {
		t.Run(category, func(t *testing.T) {
			payload := map[string]interface{}{"mentor_id": ownMentor, "mentor_full_name": "Подменённое Имя Отчество"}
			if err := handlers.validateMentor(request, category, own.partner, payload); err != nil {
				t.Fatalf("наставник своей ОО должен приниматься: %v", err)
			}
			if payload["mentor_full_name"] != sharedName {
				t.Fatalf("ФИО для отчёта берётся из справочника, получено %q", payload["mentor_full_name"])
			}

			// Наставник другого партнёра — потерянная связь.
			if err := handlers.validateMentor(request, category, own.partner, map[string]interface{}{"mentor_id": foreignMentor}); err == nil {
				t.Fatal("наставник другой ОО не должен приниматься")
			}
			// Разрешение по ФИО не должно перескакивать к тёзке чужой ОО.
			byName := map[string]interface{}{"mentor_full_name": sharedName}
			if err := handlers.validateMentor(request, category, own.partner, byName); err != nil {
				t.Fatalf("наставник своей ОО должен находиться по ФИО: %v", err)
			}
			if byName["mentor_id"] != ownMentor {
				t.Fatalf("по ФИО выбран наставник %v вместо своего %s", byName["mentor_id"], ownMentor)
			}
			// У арендатора с пустым справочником поиск по тому же ФИО не
			// должен находить чужую запись.
			blank := newTenant(ctx, t, testfixtures.New(db, t.Name()+"-blank"))
			if err := handlers.validateMentor(request, category, blank.partner, map[string]interface{}{"mentor_full_name": sharedName}); err == nil {
				t.Fatal("пустой справочник не должен подтягивать наставника другой ОО")
			}
		})
	}
}

// DASH-06 на реальной БД: дашборд изолирован по арендатору — мероприятия
// другой ИТ-компании не попадают ни в итоги, ни в разбивку по видам.
func TestDashboardIsolatesTenants(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)

	addEntry := func(tenant tenantScenario, period, amount string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,$4,$5,'vuz','{}',$6::numeric,$6::numeric,'average',$7)`,
			tenant.partner, tenant.agreement, tenant.company, period, year, amount, tenant.admin); err != nil {
			t.Fatal(err)
		}
	}
	addEntry(own, "plan", "200000")
	addEntry(own, "fact", "100000")
	addEntry(foreign, "plan", "888000")
	addEntry(foreign, "fact", "777000")

	handlers := DashboardHandlers{DB: db}
	user := middleware.AuthUser{ID: own.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &own.company}
	recorder := httptest.NewRecorder()
	handlers.Get(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard?report_year=%d", year), nil), user)
	if recorder.Code != 200 {
		t.Fatalf("дашборд вернул %d: %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		PlanTotalRub   string `json:"plan_total_rub"`
		FactTotalRub   string `json:"fact_total_rub"`
		PlanByCategory []struct {
			AmountRub string `json:"amount_rub"`
		} `json:"plan_by_category"`
		FactByCategory []struct {
			AmountRub string `json:"amount_rub"`
		} `json:"fact_by_category"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.PlanTotalRub != "200000.00" || resp.FactTotalRub != "100000.00" {
		t.Fatalf("итоги дашборда план %s / факт %s — данные другой компании просочились",
			resp.PlanTotalRub, resp.FactTotalRub)
	}
	for _, row := range append(resp.PlanByCategory, resp.FactByCategory...) {
		if row.AmountRub == "888000.00" || row.AmountRub == "777000.00" {
			t.Fatalf("в разбивку попали мероприятия другой ИТ-компании: %s", row.AmountRub)
		}
	}
	// Ответ без данных, а не чужие данные: у арендатора без мероприятий нули.
	empty := newTenant(ctx, t, f)
	emptyRecorder := httptest.NewRecorder()
	emptyUser := middleware.AuthUser{ID: empty.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &empty.company}
	handlers.Get(emptyRecorder, httptest.NewRequest("GET", fmt.Sprintf("/api/dashboard?report_year=%d", year), nil), emptyUser)
	if emptyRecorder.Code != 200 {
		t.Fatalf("дашборд пустого арендатора вернул %d: %s", emptyRecorder.Code, emptyRecorder.Body.String())
	}
	var emptyResp struct {
		PlanTotalRub string `json:"plan_total_rub"`
		FactTotalRub string `json:"fact_total_rub"`
	}
	if err := json.Unmarshal(emptyRecorder.Body.Bytes(), &emptyResp); err != nil {
		t.Fatal(err)
	}
	if emptyResp.PlanTotalRub != "0.00" || emptyResp.FactTotalRub != "0.00" {
		t.Fatalf("у арендатора без мероприятий итоги %s/%s, ожидались нули",
			emptyResp.PlanTotalRub, emptyResp.FactTotalRub)
	}
}

// WF-08 на реальной БД: Приложение № 4 строится только по неизменяемому снимку
// на 1 мая. Без снимка форма не выдаётся, а снимок с нарушенной контрольной
// суммой отвергается — подменить факт мимо снимка нельзя.
func TestAnnex4RequiresVerifiedSnapshot(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	handlers := ReportHandlers{DB: db}

	// Снимки неизменяемы, поэтому каждый случай берёт своего арендатора с
	// утверждённым планом: закрыты оба вида из соглашения, отчёт согласован.
	newPlanned := func() tenantScenario {
		t.Helper()
		tenant := newTenant(ctx, t, f)
		for _, category := range []string{"teachers", "ood_rpd"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
				audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
				VALUES($1,$2,$3,$4,'plan',$5,'vuz','{"academic_hours":64}',264960,264960,'average',$6)`,
				category, tenant.partner, tenant.agreement, tenant.company, year, tenant.admin); err != nil {
				t.Fatal(err)
			}
		}
		// Отчёт утверждается после внесения строк: триггеры сбрасывают статус
		// при любом изменении entries.
		if _, err := db.ExecContext(ctx, `INSERT INTO agreement_reports(agreement_id,report_year,period_type,status,
			scope_confirmed,conditions_confirmed,evidence_confirmed,counterparty_confirmed,approved_by,approved_at)
			VALUES($1,$2,'plan','approved',true,true,true,true,$3,now())`,
			tenant.agreement, year, tenant.admin); err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	export := func(tenant tenantScenario) *httptest.ResponseRecorder {
		t.Helper()
		user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
			EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
		recorder := httptest.NewRecorder()
		path := fmt.Sprintf("/api/reports/export?report_type=annex4&report_year=%d", year)
		handlers.Export(recorder, httptest.NewRequest("GET", path, nil), user)
		return recorder
	}
	payload := []byte(`{"schema_version":1,"entries":[],"documents":[]}`)
	sealSnapshot := func(tenant tenantScenario, hash string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO report_snapshots(it_company_id,report_year,snapshot_date,payload,payload_bytes,payload_sha256,sealed_by)
			VALUES($1,$2,make_date($2,5,1),$3::jsonb,$4,$5,$6)`,
			tenant.company, year, string(payload), payload, hash, tenant.admin); err != nil {
			t.Fatal(err)
		}
	}

	// План заполнен, но снимка факта нет — форма недоступна.
	if recorder := export(newPlanned()); recorder.Code != 409 {
		t.Fatalf("без снимка ожидался отказ 409, получено %d: %s", recorder.Code, recorder.Body.String())
	}

	// Снимок, чьи байты не сходятся с контрольной суммой, не принимается.
	tamperedTenant := newPlanned()
	tampered := sha256.Sum256([]byte(`{"schema_version":1,"entries":[{"amount_rub":"999999.00"}],"documents":[]}`))
	sealSnapshot(tamperedTenant, hex.EncodeToString(tampered[:]))
	recorder := export(tamperedTenant)
	if recorder.Code != 500 || !strings.Contains(recorder.Body.String(), "контрольная сумма") {
		t.Fatalf("снимок с нарушенной контрольной суммой должен отвергаться, получено %d: %s",
			recorder.Code, recorder.Body.String())
	}

	// Тот же снимок с верной суммой проходит: отказ выше вызван проверкой
	// целостности, а не самим наличием снимка.
	honestTenant := newPlanned()
	honest := sha256.Sum256(payload)
	sealSnapshot(honestTenant, hex.EncodeToString(honest[:]))
	if recorder := export(honestTenant); recorder.Code != 200 {
		t.Fatalf("проверенный снимок должен давать форму, получено %d: %s", recorder.Code, recorder.Body.String())
	}
}
