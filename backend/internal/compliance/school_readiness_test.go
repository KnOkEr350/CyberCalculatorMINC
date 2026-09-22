package compliance

import "testing"

// SCH-07: готовность школьного трека собирается из соглашения с РОИВ или
// школой, сформированных групп участников, акта сдачи-приёмки, а для
// цифрового контента — ещё и подтверждённых логов ФГИС «Моя школа».
func TestSchoolReadinessCombinesAgreementGroupsActAndLogs(t *testing.T) {
	for _, category := range []string{"it_clubs", "teacher_training"} {
		t.Run(category, func(t *testing.T) {
			full := Evaluate(category, "fact", map[string]interface{}{}, schoolDocuments())
			if full.State != "green" || !full.Ready || !full.Eligible {
				t.Fatalf("полный комплект должен быть готов и допустим: %+v", full)
			}
			// Соглашение и группы — основания зачёта, их отсутствие блокирует.
			for _, document := range []string{"school_agreement", "participant_groups"} {
				got := Evaluate(category, "fact", map[string]interface{}{}, without(schoolDocuments(), document))
				if got.State != "red" || got.Eligible {
					t.Fatalf("без документа %q зачёт невозможен: %+v", document, got)
				}
			}
			// Акт — закрывающий документ: без него строка в зоне внимания.
			act := Evaluate(category, "fact", map[string]interface{}{}, without(schoolDocuments(), "acceptance_act"))
			if act.State != "yellow" {
				t.Fatalf("без акта сдачи-приёмки ожидалась жёлтая зона: %+v", act)
			}
		})
	}

	// Вид 8 дополнительно требует подтверждённый цифровой след.
	trace := map[string]interface{}{
		"digital_trace_period_start": "2026-01-01", "digital_trace_period_end": "2026-05-01",
		"digital_trace_participants": 45,
		"digital_trace_sha256":       "9f2c1f8b7d6e5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
	}
	contentDocuments := []string{"school_agreement", "digital_trace", "acceptance_act"}
	if got := Evaluate("edu_content", "fact", trace, contentDocuments); got.State != "green" {
		t.Fatalf("полный комплект Вида 8 должен быть зелёным: %+v", got)
	}
	noLogs := Evaluate("edu_content", "fact", trace, without(contentDocuments, "digital_trace"))
	if noLogs.State != "red" {
		t.Fatalf("без выгрузки логов ФГИС зачёт невозможен: %+v", noLogs)
	}
}

// Плановые строки школьного трека не блокируются отсутствием закрывающих
// документов: занятия ещё не прошли (ADR-18).
func TestSchoolPlanRowsAreNotBlocked(t *testing.T) {
	for _, category := range []string{"it_clubs", "teacher_training", "edu_content"} {
		got := Evaluate(category, "plan", map[string]interface{}{}, nil)
		if got.State == "red" {
			t.Fatalf("%s: плановая строка не должна блокироваться: %+v", category, got)
		}
		if got.Ready {
			t.Fatalf("%s: плановая строка без документов не может считаться готовой", category)
		}
	}
}
