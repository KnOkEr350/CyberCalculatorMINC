package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

func TestBuildPlanFactRowsShowsDashPercentWithoutPlan(t *testing.T) {
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки", Period: "fact", Amount: money.Amount(150_000_00)},
	}
	rows := buildPlanFactRows(data)
	if len(rows) != 1 {
		t.Fatalf("ожидали 1 строку, получили %d: %+v", len(rows), rows)
	}
	if percent := rows[0][5]; percent != "—" {
		t.Fatalf("при плане=0 и факте>0 процент дельты должен быть прочерком, получили %v", percent)
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

// REPORT-06: Приложение № 2 сводит стажёров в программы стажировок, как
// того требует форма, и закрывается строкой «ИТОГО».
func TestBuildAnnex2RowsGroupsByProgram(t *testing.T) {
	intern := func(student string, hours float64) []byte {
		payload, _ := json.Marshal(map[string]interface{}{
			"internship_agreement_reference": "Договор о стажировке № 7",
			"student_full_name":              student, "mentor_full_name": "Васильев М.А.",
			"total_student_hours": hours, "total_mentor_hours": 20,
		})
		return payload
	}
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки", Period: "fact", Amount: money.Amount(33540000), Payload: intern("Архипов Д.С.", 240)},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки", Period: "fact", Amount: money.Amount(33540000), Payload: intern("Белов Е.В.", 240)},
	}
	rows := buildAnnex2Rows(data)
	if len(rows) != 2 { // одна программа плюс ИТОГО
		t.Fatalf("две стажировки одной программы должны дать одну строку: %+v", rows)
	}
	row := rows[0]
	if row[1] != "Договор о стажировке № 7" {
		t.Fatalf("наименование программы = %v", row[1])
	}
	if row[2] != "астрономический час" || row[4].(float64) != 480 {
		t.Fatalf("метрика и часы неверны: %v / %v", row[2], row[4])
	}
	if amount := row[6].(float64); amount != 670.8 {
		t.Fatalf("сумма по программе = %v тыс. руб., ожидалось 670.8", amount)
	}
	if info := row[7].(string); !strings.Contains(info, "стажёров: 2") {
		t.Fatalf("в дополнительной информации нет числа стажёров: %q", info)
	}
	if rows[1][1] != "ИТОГО" {
		t.Fatalf("нет итоговой строки: %+v", rows[1])
	}
}

// ТЗ, п. 9.2: специализированный «Отчёт по наставникам».
func TestBuildMentorRowsAggregatesByMentor(t *testing.T) {
	payload := func(mentor, student string) []byte {
		body, _ := json.Marshal(map[string]interface{}{"mentor_full_name": mentor, "student_full_name": student, "total_mentor_hours": 20})
		return body
	}
	noMentor, _ := json.Marshal(map[string]interface{}{"student_full_name": "Кириллов В.О."})
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", Period: "fact", Amount: money.Amount(33540000), Payload: payload("Васильев М.А.", "Архипов Д.С.")},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", Period: "fact", Amount: money.Amount(33540000), Payload: payload("Васильев М.А.", "Белов Е.В.")},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", Period: "fact", Amount: money.Amount(24000000), Payload: noMentor},
	}
	rows := buildMentorRows(data)
	if len(rows) != 2 { // один наставник плюс строка «ИТОГО» (INT-09)
		t.Fatalf("ожидали строку наставника и итог, получили %+v", rows)
	}
	row := rows[0]
	if row[1] != "Васильев М.А." || row[3] != 2 {
		t.Fatalf("наставник и число стажёров неверны: %+v", row)
	}
	// Ставка наставника начисляется за каждого стажёра персонально: 20 ч × 2.
	if hours := row[4].(float64); hours != 40 {
		t.Fatalf("часы сопровождения = %v, ожидалось 40", hours)
	}
	// Мероприятие без наставника в срез не попадает, поэтому итог считается
	// только по строкам наставников.
	if totals := rows[1]; totals[1] != "ИТОГО" || totals[5].(float64) != row[5].(float64) {
		t.Fatalf("итоговая строка не сходится с единственным наставником: %+v", totals)
	}
}

// INT-06: часы наставника не должны удваиваться, если заполнены и итог, и
// помесячная нагрузка.
func TestMentorHoursDoesNotDoubleCount(t *testing.T) {
	both := map[string]interface{}{"total_mentor_hours": json.Number("60"), "mentor_load_hours_per_month": json.Number("20"), "duration_months": json.Number("3")}
	if got := mentorHours(both); got != 60 {
		t.Fatalf("mentorHours() = %v, ожидалось 60 (готовый итог)", got)
	}
	monthly := map[string]interface{}{"mentor_load_hours_per_month": json.Number("20"), "duration_months": json.Number("3")}
	if got := mentorHours(monthly); got != 60 {
		t.Fatalf("mentorHours() = %v, ожидалось 60 (20 ч × 3 мес.)", got)
	}
}

// Регрессия: неполный payload не должен давать в ячейках литерал "<nil>"
// ни в одной из форм, построенных из мероприятий.
func TestRegulatoryRowsNeverPrintNilLiteral(t *testing.T) {
	sparse, _ := json.Marshal(map[string]interface{}{"student_full_name": "Сидоров П.В."})
	data := []regulatoryRow{{PartnerID: "p1", Partner: "МГУ", Category: "internship", CategoryName: "Стажировки",
		Period: "fact", Amount: money.Amount(100000), Payload: sparse}}
	for name, rows := range map[string][][]interface{}{
		"Приложение № 1":       flattenSheets(buildAnnex1Sheets(data)),
		"Приложение № 2":       buildAnnex2Rows(data),
		"Отчёт по наставникам": buildMentorRows(data),
	} {
		for _, row := range rows {
			for index, cell := range row {
				if fmt.Sprint(cell) == "<nil>" {
					t.Fatalf("%s: ячейка %d напечатана как \"<nil>\": %+v", name, index, row)
				}
			}
		}
	}
}

func flattenSheets(sheets []annex1Sheet) [][]interface{} {
	rows := [][]interface{}{}
	for _, sheet := range sheets {
		rows = append(rows, sheet.Rows...)
	}
	return rows
}

// REPORT-05: Приложение № 1 к Отчёту заполняется отдельно по каждой ОО или
// РОИВ, суммы приводятся в тыс. руб., а лист закрывается строкой «ИТОГО».
func TestBuildAnnex1SheetsPerOrganization(t *testing.T) {
	teachersPayload, _ := json.Marshal(map[string]interface{}{"course_name": "Архитектура ИС", "academic_hours": 64})
	programPayload, _ := json.Marshal(map[string]interface{}{"program_name": "09.03.01 Информационная безопасность"})
	data := []regulatoryRow{
		{PartnerID: "p2", Partner: "МФТИ", Category: "ood_rpd", CategoryName: "ООП и РПД", Period: "fact",
			Amount: money.Amount(203985000), Payload: programPayload},
		{PartnerID: "p1", Partner: "МГУ", Category: "teachers", CategoryName: "Преподаватели-практики", Period: "fact",
			Amount: money.Amount(26496000), Payload: teachersPayload},
	}
	sheets := buildAnnex1Sheets(data)
	if len(sheets) != 2 {
		t.Fatalf("ожидали отдельный лист на каждую ОО, получили %d: %+v", len(sheets), sheets)
	}
	if sheets[0].Name != "МГУ" || sheets[1].Name != "МФТИ" {
		t.Fatalf("листы названы именами ОО и отсортированы: %q, %q", sheets[0].Name, sheets[1].Name)
	}
	row := sheets[0].Rows[0]
	if row[0] != 1 || row[1] != "Преподаватели-практики" {
		t.Fatalf("начало строки неверно: %+v", row)
	}
	if row[2] != "Архитектура ИС" {
		t.Fatalf("графа «Мероприятие» = %v, ожидалось наименование дисциплины", row[2])
	}
	if row[3] != "академический час" || row[4] != "Часы преподавания" {
		t.Fatalf("метрика и показатель объёма неверны: %v / %v", row[3], row[4])
	}
	if volume := row[5].(float64); volume != 64 {
		t.Fatalf("значение показателя = %v, ожидалось 64 часа", volume)
	}
	// 264 960 ₽ за 64 часа — это тариф ВО 4,14 тыс. руб. за час.
	if unitCost := row[6].(float64); unitCost != 4.14 {
		t.Fatalf("стоимость единицы = %v тыс. руб., ожидалось 4.14", unitCost)
	}
	if amount := row[7].(float64); amount != 264.96 {
		t.Fatalf("сумма затрат = %v тыс. руб., ожидалось 264.96", amount)
	}
	totals := sheets[0].Rows[len(sheets[0].Rows)-1]
	if totals[1] != "ИТОГО" || totals[7].(float64) != 264.96 {
		t.Fatalf("итоговая строка листа неверна: %+v", totals)
	}
}

func TestBuildAnnex1SheetsSkipsEmptyRowsAndUnknownPartner(t *testing.T) {
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "", Category: "teachers", CategoryName: "Преподаватели-практики", Period: "fact", Amount: money.Amount(100000)},
		{PartnerID: "p1", Partner: "", Category: "teachers", CategoryName: "Преподаватели-практики", Period: "fact", Amount: 0},
	}
	sheets := buildAnnex1Sheets(data)
	if len(sheets) != 1 || sheets[0].Name != "Без контрагента" {
		t.Fatalf("ожидали один лист «Без контрагента», получили %+v", sheets)
	}
	if len(sheets[0].Rows) != 2 { // одна запись плюс ИТОГО
		t.Fatalf("нулевая строка должна быть удалена: %+v", sheets[0].Rows)
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
