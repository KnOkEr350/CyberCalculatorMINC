package handlers

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"cybercalc/internal/money"
)

func TestRegulatoryHeadersGolden(t *testing.T) {
	actual, err := json.Marshal(regulatoryHeaders)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/regulatory_headers.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, bytes.TrimSpace(expected)) {
		t.Fatalf("regulatory report headers differ from golden file\nactual: %s", actual)
	}
}

// REPORT-07: Приложение № 3 должно содержать читаемую формулировку
// «Соглашение с <ОО> от <дата> № <номер>», а не сырые UUID.
func TestFormatRuDate(t *testing.T) {
	cases := map[string]string{
		"2026-05-01": "01.05.2026",
		"2026-12-31": "31.12.2026",
		"":           "",           // пусто — не наша забота, вернуть как есть
		"not-a-date": "not-a-date", // нераспознанный формат возвращается как есть
	}
	for in, want := range cases {
		if got := formatRuDate(in); got != want {
			t.Errorf("formatRuDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatAbsenceStatement(t *testing.T) {
	got := formatAbsenceStatement("МФТИ (НИУ)", "2026-01-20", "№ 14-СОГЛ")
	want := "Соглашение с МФТИ (НИУ) от 20.01.2026 № № 14-СОГЛ"
	if got != want {
		t.Fatalf("formatAbsenceStatement() = %q, want %q", got, want)
	}
}

// REPORT-10: конструктор срезов должен убирать пустые строки (план=0 и
// факт=0), как того требует «Правило выгрузки XLSX» Приказа № 270,
// независимо от настроек фильтрации в UI.
func TestBuildAnnex5RowsDropsZeroRows(t *testing.T) {
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Period: "plan", Amount: money.Amount(100000)},
		{PartnerID: "p1", Partner: "МГУ", Period: "fact", Amount: money.Amount(100000)},
		// p2 имеет только план и только факт по разным категориям — итог по
		// контрагенту не нулевой, строка должна остаться.
		{PartnerID: "p2", Partner: "МФТИ", Period: "plan", Amount: money.Amount(50000)},
		// p3 присутствует в выборке, но план и факт взаимно нулевые не
		// возникают в реальных данных; проверяем именно "оба по нулю" —
		// сконструируем это явно через нулевую сумму.
		{PartnerID: "p3", Partner: "Колледж связи № 54", Period: "plan", Amount: money.Amount(0)},
		{PartnerID: "p3", Partner: "Колледж связи № 54", Period: "fact", Amount: money.Amount(0)},
	}
	rows, planTotal, factTotal := buildAnnex5Rows(data)
	if len(rows) != 2 {
		t.Fatalf("ожидали 2 непустые строки (p1, p2), получили %d: %+v", len(rows), rows)
	}
	for _, row := range rows {
		if row[1] == "Колледж связи № 54" {
			t.Fatalf("нулевая строка p3 не была удалена: %+v", row)
		}
	}
	// Номера строк ("№") идут подряд без пропусков после удаления p3.
	if rows[0][0] != 1 || rows[1][0] != 2 {
		t.Fatalf("нумерация строк не пересчитана после удаления пустых: %+v", rows)
	}
	// Итоги считаются по всем контрагентам, включая удалённую нулевую строку.
	if planTotal != money.Amount(150000) || factTotal != money.Amount(100000) {
		t.Fatalf("итоги план/факт неверны: plan=%v fact=%v", planTotal, factTotal)
	}
}

func TestBuildPlanFactRowsDropsZeroRows(t *testing.T) {
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Category: "teachers", CategoryName: "Преподаватели", Period: "plan", Amount: money.Amount(10000)},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки", Period: "plan", Amount: money.Amount(0)},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки", Period: "fact", Amount: money.Amount(0)},
	}
	rows := buildPlanFactRows(data)
	if len(rows) != 1 {
		t.Fatalf("ожидали 1 непустую строку (teachers), получили %d: %+v", len(rows), rows)
	}
	if rows[0][1] != "Преподаватели" {
		t.Fatalf("осталась не та строка: %+v", rows[0])
	}
}

// REPORT-11: реестр сформированных файлов должен хранить реально
// применённые параметры выгрузки (не только партнёра и соглашение), а
// пустые/невыбранные фильтры не должны засорять запись как "".
func TestReportFiltersDropsEmptyValues(t *testing.T) {
	got := reportFilters(
		"partner_id", "p-1",
		"agreement_id", "",
		"period_type", "fact",
		"category_code", "",
		"mentor_id", "m-1",
	)
	want := map[string]string{"partner_id": "p-1", "period_type": "fact", "mentor_id": "m-1"}
	if len(got) != len(want) {
		t.Fatalf("reportFilters() = %+v, want %+v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("reportFilters()[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["agreement_id"]; ok {
		t.Fatalf("пустой agreement_id не должен попадать в фильтры: %+v", got)
	}
	if _, ok := got["category_code"]; ok {
		t.Fatalf("пустой category_code не должен попадать в фильтры: %+v", got)
	}
}

func TestReportFiltersEmptyInput(t *testing.T) {
	if got := reportFilters(); len(got) != 0 {
		t.Fatalf("reportFilters() без аргументов = %+v, ожидали пустую карту", got)
	}
	if got := reportFilters("partner_id", ""); len(got) != 0 {
		t.Fatalf("reportFilters(\"partner_id\", \"\") = %+v, ожидали пустую карту", got)
	}
}
