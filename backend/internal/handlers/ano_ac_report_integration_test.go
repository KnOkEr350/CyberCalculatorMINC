package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
	"cybercalc/internal/topit"
	"cybercalc/internal/xlsx"
)

// REPORT-09: отчёт АНО АЦ — шесть листов по ТЗ, данные программы и её
// составляющих, шкала, изоляция арендаторов и запись в реестр файлов.
func TestAnoAcReportHasSixSheetsAndIsTenantScoped(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	own, foreign := newTenant(ctx, t, f), newTenant(ctx, t, f)
	var entry string
	if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,audience,payload,
			amount_rub,formula_amount_rub,cost_method,created_by)
		VALUES('top_it',$1,$2,$3,'fact',$4,'vuz','{"project_name":"Цифровая кафедра","program_name":"ТОП-ИТ Программа","program_wave":"2","partner_role":"anchor",
			"grant_amount_rub":"5000000","planned_cofinancing_amount_rub":"5000000","actual_spent_amount_rub":"3800000"}'::jsonb,
			100000,100000,'average',$5) RETURNING id::text`, own.partner, own.agreement, own.company, year, own.admin).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	share := func(v float64) *float64 { return &v }
	if _, err := topit.Create(ctx, db, entry, topit.Input{Kind: "rid", Title: "SecureAI-BERT", RIDType: "ai_model", Authors: "Иванов И. И.",
		UniversitySharePct: share(50), CompanySharePct: share(50)}, own.admin); err != nil {
		t.Fatal(err)
	}
	handler := ReportHandlers{DB: db}
	export := func(tenant tenantScenario, query string) *httptest.ResponseRecorder {
		user := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
		rec := httptest.NewRecorder()
		handler.Export(rec, httptest.NewRequest("GET", fmt.Sprintf("/api/reports/export?report_type=ano_ac&report_year=%d%s", year, query), nil), user)
		return rec
	}
	rec := export(own, "&entry_id="+entry)
	if rec.Code != 200 {
		t.Fatalf("отчёт: %d %s", rec.Code, rec.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var workbook string
	for _, file := range archive.File {
		if file.Name == "xl/workbook.xml" {
			reader, _ := file.Open()
			data, _ := io.ReadAll(reader)
			reader.Close()
			workbook = string(data)
		}
	}
	for _, sheet := range []string{"Паспорт", "Денежное софинансирование", "Неденежная поддержка", "Стипендиаты", "Кейсы", "РИД"} {
		if !strings.Contains(workbook, `name="`+sheet+`"`) {
			t.Errorf("нет листа %q", sheet)
		}
	}
	rows, err := xlsx.ReadFirst(rec.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	passport := fmt.Sprint(rows)
	for _, want := range []string{"ТОП-ИТ Программа", "Якорный", "5000000.00", "3500000.00"} { // норма вуза 70% от 5 млн
		if !strings.Contains(passport, want) {
			t.Errorf("паспорт не содержит %q: %s", want, passport)
		}
	}
	var registered int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM generated_reports WHERE report_type='ano_ac' AND it_company_id::text=$1`, own.company).Scan(&registered); err != nil || registered != 1 {
		t.Fatalf("файл должен попасть в реестр: %d %v", registered, err)
	}
	if bad := export(foreign, "&entry_id="+entry); bad.Code != 404 {
		t.Fatalf("чужая программа не выгружается: %d", bad.Code)
	}
	if bad := export(own, ""); bad.Code != 400 {
		t.Fatalf("без entry_id — 400: %d", bad.Code)
	}
}
