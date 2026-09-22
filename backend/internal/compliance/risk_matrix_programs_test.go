package compliance

import "testing"

// RISK-04 / RISK-05 / RISK-06: продолжение матрицы зон риска ТЗ (§5.1).
//
// Вид 3 (ООП и РПД):
//
//	🔴 ОО не предоставила проект ИЛИ не назначен эксперт ИЛИ нет соглашения с ОО;
//	🟡 проект предоставлен и эксперт назначен, но заключение не подписано ИЛИ Учёный совет не утвердил;
//	🟢 полный пакет: заключение с подписью компании и выписка Учёного совета.
//
// Вид 4 (ТОП-ИТ / ТОП-ИИ):
//
//	🔴 договор не подписан ИЛИ средства не переведены ИЛИ освоено менее 70%;
//	🟡 средства переведены и отчёт принят, но нет письма АНО АЦ либо освоено 70–99.9%;
//	🟢 освоено ≥100% и получено письмо-согласование АНО АЦ.
//
// Виды 6–8 (школьный трек):
//
//	🔴 нет соглашения с РОИВ/школой ИЛИ не сформированы группы учащихся;
//	🟡 занятия ведутся, но нет промежуточного акта;
//	🟢 соглашение, группы, акт сдачи-приёмки и подтверждённые логи.

func programPayload() map[string]interface{} {
	return map[string]interface{}{
		"doc_type": "rpd", "level": "vo", "activity_type": "expertise",
		"program_name": "РПД «Безопасность облачных систем»", "expert_full_name": "Семёнов К.А.",
	}
}

func programDocuments() []string {
	return []string{"program_project", "expert_conclusion", "academic_council_protocol"}
}

func TestRiskMatrixProgramsAndDisciplines(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]interface{})
		documents []string
		want      string
	}{
		{"полный пакет — зелёная зона", nil, programDocuments(), "green"},
		{"ОО не предоставила проект — красная зона",
			nil, without(programDocuments(), "program_project"), "red"},
		{"эксперт не назначен для экспертизы — красная зона",
			func(p map[string]interface{}) { delete(p, "expert_full_name") }, programDocuments(), "red"},
		{"заключение не подписано — жёлтая зона",
			nil, without(programDocuments(), "expert_conclusion"), "yellow"},
		{"Учёный совет не утвердил — жёлтая зона",
			nil, without(programDocuments(), "academic_council_protocol"), "yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := programPayload()
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			got := Evaluate("ood_rpd", "fact", payload, tc.documents)
			if got.State != tc.want {
				t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
			}
		})
	}
}

// Разработка и актуализация идут без назначенного эксперта: требование
// эксперта относится к экспертизе (текущая трактовка движка).
func TestProgramDevelopmentDoesNotRequireExpert(t *testing.T) {
	for _, activity := range []string{"development", "update"} {
		payload := programPayload()
		payload["activity_type"] = activity
		delete(payload, "expert_full_name")
		if got := Evaluate("ood_rpd", "fact", payload, programDocuments()); got.State != "green" {
			t.Fatalf("%s: зона %q, ожидалась зелёная (blocking=%v)", activity, got.State, got.Blocking)
		}
	}
}

func topPayload(spent float64) map[string]interface{} {
	return map[string]interface{}{
		"planned_cofinancing_amount_rub": 1000.0,
		"transferred_amount_rub":         1000.0,
		"actual_spent_amount_rub":        spent,
	}
}

func topDocuments() []string {
	return []string{"top_agreement", "payment_order", "spending_act", "ano_letter"}
}

func TestRiskMatrixTopIT(t *testing.T) {
	cases := []struct {
		name      string
		spent     float64
		documents []string
		want      string
	}{
		{"освоено 100% и есть письмо АЦ — зелёная зона", 1000, topDocuments(), "green"},
		{"освоено больше плана — зелёная зона", 1200, topDocuments(), "green"},
		// ТЗ §7.5: 70–99.9% — зона внимания, а не выполнение.
		{"освоено 70% — жёлтая зона внимания", 700, topDocuments(), "yellow"},
		{"освоено 99.9% — жёлтая зона внимания", 999, topDocuments(), "yellow"},
		{"освоено меньше 70% — красная зона", 699, topDocuments(), "red"},
		{"договор не подписан — красная зона", 1000, without(topDocuments(), "top_agreement"), "red"},
		{"средства не переведены — красная зона", 1000, without(topDocuments(), "payment_order"), "red"},
		{"отчёт о списании не предоставлен — красная зона", 1000, without(topDocuments(), "spending_act"), "red"},
		{"нет письма АНО АЦ — жёлтая зона", 1000, without(topDocuments(), "ano_letter"), "yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate("top_it", "fact", topPayload(tc.spent), tc.documents)
			if got.State != tc.want {
				t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
			}
		})
	}
}

func schoolDocuments() []string {
	return []string{"school_agreement", "participant_groups", "acceptance_act"}
}

func TestRiskMatrixSchoolTrack(t *testing.T) {
	for _, category := range []string{"it_clubs", "teacher_training"} {
		cases := []struct {
			name      string
			documents []string
			want      string
		}{
			{"соглашение, группы и акт — зелёная зона", schoolDocuments(), "green"},
			{"нет соглашения со школой или РОИВ — красная зона", without(schoolDocuments(), "school_agreement"), "red"},
			{"группы учащихся не сформированы — красная зона", without(schoolDocuments(), "participant_groups"), "red"},
			{"нет акта сдачи-приёмки — жёлтая зона", without(schoolDocuments(), "acceptance_act"), "yellow"},
		}
		for _, tc := range cases {
			t.Run(category+": "+tc.name, func(t *testing.T) {
				got := Evaluate(category, "fact", map[string]interface{}{}, tc.documents)
				if got.State != tc.want {
					t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
				}
			})
		}
	}
}

// Плановые строки могут опережать закрывающие документы: по ним отсутствие
// комплекта даёт предупреждение, а не блокировку (ADR-18).
func TestPlanRowsWarnInsteadOfBlocking(t *testing.T) {
	got := Evaluate("it_clubs", "plan", map[string]interface{}{}, nil)
	if got.State != "yellow" {
		t.Fatalf("плановая строка без документов должна быть жёлтой, получено %q (blocking=%v)", got.State, got.Blocking)
	}
	if fact := Evaluate("it_clubs", "fact", map[string]interface{}{}, nil); fact.State != "red" {
		t.Fatalf("фактическая строка без документов должна быть красной, получено %q", fact.State)
	}
}
