package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// REPORT-04: Таблица 1 Приложения № 5 считает процент от единого норматива
// 3% компании (а не от собственного плана контрагента, которого в
// официальной форме вообще нет), собирает реквизиты соглашений контрагента
// и переводит суммы в тыс. руб. Контрагенты без факта (пустые строки)
// программно удаляются независимо от настроек фильтрации в UI (REPORT-10).
func TestBuildAnnex5RowsDropsZeroRows(t *testing.T) {
	target := money.Amount(1_000_000_00) // 1 000 000 ₽ = норматив 3% компании
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Agreement: "№ 01/26-МЦ", Period: "plan", Amount: money.Amount(200_000_00)},
		{PartnerID: "p1", Partner: "МГУ", Agreement: "№ 01/26-МЦ", Period: "fact", Amount: money.Amount(300_000_00)},
		// Второе соглашение того же партнёра: реквизиты должны попасть в
		// строку p1 оба, без дублей.
		{PartnerID: "p1", Partner: "МГУ", Agreement: "№ 01/26-МЦ", Period: "fact", Amount: money.Amount(50_000_00)},
		{PartnerID: "p1", Partner: "МГУ", Agreement: "Доп. соглашение № 2", Period: "fact", Amount: money.Amount(50_000_00)},
		// p2 — только план, факта нет: пустая строка, должна быть удалена.
		{PartnerID: "p2", Partner: "Колледж связи № 54", Agreement: "№ СПО-54/А", Period: "plan", Amount: money.Amount(80_000_00)},
	}
	rows, planTotal, factTotal := buildAnnex5Rows(data, target)
	if len(rows) != 1 {
		t.Fatalf("ожидали 1 непустую строку (p1), получили %d: %+v", len(rows), rows)
	}
	row := rows[0]
	if row[1] != "МГУ" {
		t.Fatalf("осталась не та строка: %+v", row)
	}
	if agreements := row[2].(string); agreements != "№ 01/26-МЦ; Доп. соглашение № 2" {
		t.Fatalf("реквизиты соглашений собраны неверно: %q", agreements)
	}
	if targetThousand := row[3].(float64); targetThousand != 1000 {
		t.Fatalf("норматив в тыс. руб. = %v, want 1000", targetThousand)
	}
	// Факт p1 = 300 000 + 50 000 + 50 000 = 400 000 ₽ = 400 тыс. руб.
	if factThousand := row[4].(float64); factThousand != 400 {
		t.Fatalf("факт в тыс. руб. = %v, want 400", factThousand)
	}
	// Процент считается от норматива компании (1 000 000), а не от
	// собственного плана p1 (200 000): 400 000 / 1 000 000 * 100 = 40%.
	if percent := row[5].(float64); percent != 40 {
		t.Fatalf("процент от норматива = %v, want 40 (не от собственного плана контрагента)", percent)
	}
	if planTotal != money.Amount(280_000_00) || factTotal != money.Amount(400_000_00) {
		t.Fatalf("итоги план/факт для информационного листа неверны: plan=%v fact=%v", planTotal, factTotal)
	}
}

func TestBuildAnnex5RowsRoundsPercentToHundredths(t *testing.T) {
	// 1 / 3 норматива = 33.333…% — в ячейку формы должно попасть 33.33.
	data := []regulatoryRow{{PartnerID: "p1", Partner: "МГУ", Period: "fact", Amount: money.Amount(100)}}
	rows, _, _ := buildAnnex5Rows(data, money.Amount(300))
	if percent := rows[0][5].(float64); percent != 33.33 {
		t.Fatalf("процент от норматива = %v, want 33.33", percent)
	}
}

func TestBuildAnnex5RowsWithoutTargetShowsDash(t *testing.T) {
	data := []regulatoryRow{{PartnerID: "p1", Partner: "МГУ", Period: "fact", Amount: money.Amount(100000)}}
	rows, _, _ := buildAnnex5Rows(data, 0)
	if len(rows) != 1 {
		t.Fatalf("ожидали 1 строку, получили %d", len(rows))
	}
	if percent := rows[0][5]; percent != "—" {
		t.Fatalf("без настроенного норматива 3%% процент должен быть прочерком, получили %v", percent)
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

// Регрессия: fmt.Sprint(p[key]) на отсутствующем ключе печатает буквальное
// "<nil>" в ячейку регламентной формы вместо пустой строки.
func TestPayloadValueMissingKeyIsEmptyNotNilString(t *testing.T) {
	p := map[string]interface{}{"student_full_name": "Иванов И.И.", "labor_contract_number": nil}
	if got := payloadValue(p, "student_full_name"); got != "Иванов И.И." {
		t.Fatalf("payloadValue() = %q, want %q", got, "Иванов И.И.")
	}
	if got := payloadValue(p, "mentor_full_name"); got != "" {
		t.Fatalf("payloadValue() для отсутствующего ключа = %q, want \"\" (не \"<nil>\")", got)
	}
	if got := payloadValue(p, "labor_contract_number"); got != "" {
		t.Fatalf("payloadValue() для nil-значения = %q, want \"\" (не \"<nil>\")", got)
	}
}

func TestBuildAnnex2RowsNoNilLiteral(t *testing.T) {
	payload, _ := json.Marshal(map[string]interface{}{"student_full_name": "Сидоров П.В.", "duration_months": 3})
	data := []regulatoryRow{{Partner: "МГУ", Amount: money.Amount(100000), Payload: payload}}
	rows := buildAnnex2Rows(data)
	if len(rows) != 1 {
		t.Fatalf("ожидали 1 строку, получили %d", len(rows))
	}
	row := rows[0]
	if row[2] != "Сидоров П.В." {
		t.Fatalf("студент = %v, want Сидоров П.В.", row[2])
	}
	// mentor_full_name и labor_contract_number отсутствуют в payload — не
	// должны стать строкой "<nil>".
	for _, idx := range []int{3, 7} {
		if v := fmt.Sprint(row[idx]); v == "<nil>" {
			t.Fatalf("row[%d] = %q, отсутствующее поле не должно печататься как \"<nil>\"", idx, v)
		}
		if row[idx] != "" {
			t.Fatalf("row[%d] = %v, want \"\"", idx, row[idx])
		}
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
