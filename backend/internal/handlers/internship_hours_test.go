package handlers

import (
	"encoding/json"
	"testing"

	"cybercalc/internal/money"
)

// INT-06: часы стажировки нормализуются один раз. Запись может хранить либо
// готовый итог, либо помесячную нагрузку и срок; складывать оба
// представления нельзя, иначе объём в регламентных формах удвоится.
func TestInternshipHoursNormalisedOnce(t *testing.T) {
	cases := []struct {
		name                    string
		payload                 map[string]interface{}
		wantStudent, wantMentor float64
	}{
		{
			name: "готовые итоги имеют приоритет над помесячной нагрузкой",
			payload: map[string]interface{}{
				"total_student_hours": json.Number("240"), "total_mentor_hours": json.Number("60"),
				"student_load_hours_per_month": json.Number("80"), "mentor_load_hours_per_month": json.Number("20"),
				"duration_months": json.Number("3"),
			},
			wantStudent: 240, wantMentor: 60,
		},
		{
			name: "без итогов часы считаются из нагрузки и срока",
			payload: map[string]interface{}{
				"student_load_hours_per_month": json.Number("80"), "mentor_load_hours_per_month": json.Number("20"),
				"duration_months": json.Number("3"),
			},
			wantStudent: 240, wantMentor: 60,
		},
		{
			name:        "пустая запись даёт нулевые часы, а не ошибку",
			payload:     map[string]interface{}{},
			wantStudent: 0, wantMentor: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := studentHours(tc.payload); got != tc.wantStudent {
				t.Fatalf("часы студента = %v, ожидалось %v", got, tc.wantStudent)
			}
			if got := mentorHours(tc.payload); got != tc.wantMentor {
				t.Fatalf("часы наставника = %v, ожидалось %v", got, tc.wantMentor)
			}
		})
	}
}

// Объём стажировки в регламентных формах берётся из той же нормализации:
// иначе Приложение № 4 и Приложение № 2 разойдутся между собой.
func TestActivityMetricsUsesNormalisedInternshipHours(t *testing.T) {
	both := map[string]interface{}{
		"total_student_hours":          json.Number("240"),
		"student_load_hours_per_month": json.Number("80"), "duration_months": json.Number("3"),
	}
	for _, category := range []string{"internship", "employment_practice"} {
		volume, _, unit := activityMetrics(category, both)
		if volume != 240 {
			t.Fatalf("%s: объём %v, ожидалось 240 — часы не должны удваиваться", category, volume)
		}
		if unit != "человеко-час" {
			t.Fatalf("%s: единица измерения %q", category, unit)
		}
	}
}

// INT-09: срез по наставникам должен сходиться с суммой тех же мероприятий.
func TestMentorReportReconcilesWithTotals(t *testing.T) {
	payload := func(mentor, student string, hours int) []byte {
		body, _ := json.Marshal(map[string]interface{}{
			"mentor_full_name": mentor, "student_full_name": student, "total_mentor_hours": hours,
		})
		return body
	}
	data := []regulatoryRow{
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", Period: "fact", Amount: money.Amount(33540000), Payload: payload("Васильев М.А.", "Архипов Д.С.", 20)},
		{PartnerID: "p1", Partner: "МГУ", Category: "internship", Period: "fact", Amount: money.Amount(33540000), Payload: payload("Васильев М.А.", "Белов Е.В.", 20)},
		{PartnerID: "p2", Partner: "МФТИ", Category: "internship", Period: "fact", Amount: money.Amount(41925000), Payload: payload("Григорьев П.С.", "Зайцев Н.А.", 15)},
	}
	rows := buildMentorRows(data)
	if len(rows) != 3 { // два наставника плюс ИТОГО
		t.Fatalf("ожидали две строки наставников и итог, получили %d: %+v", len(rows), rows)
	}
	totals := rows[len(rows)-1]
	if totals[1] != "ИТОГО" {
		t.Fatalf("последняя строка должна быть итоговой: %+v", totals)
	}

	// Итог среза обязан совпасть с суммой исходных мероприятий.
	var expected money.Amount
	for _, row := range data {
		expected, _ = money.Add(expected, row.Amount)
	}
	if got := totals[5].(float64); got != thousandRub(expected) {
		t.Fatalf("итог по наставникам %v тыс. руб. не сходится с суммой мероприятий %v тыс. руб.", got, thousandRub(expected))
	}
	// Сходимость построчно: сумма строк равна итогу.
	var rowSum float64
	students, hours := 0, 0.0
	for _, row := range rows[:len(rows)-1] {
		rowSum += row[5].(float64)
		students += row[3].(int)
		hours += row[4].(float64)
	}
	if rowSum != totals[5].(float64) {
		t.Fatalf("сумма строк %v не равна итогу %v", rowSum, totals[5])
	}
	if students != totals[3].(int) || hours != totals[4].(float64) {
		t.Fatalf("стажёры и часы в итоге не сходятся со строками: %+v", totals)
	}
}
