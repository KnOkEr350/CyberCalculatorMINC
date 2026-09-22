package compliance

import "testing"

// RISK-02 / RISK-03: матрица зон риска из ТЗ (§5.1) построчно.
//
// Вид 1 (преподаватели):
//
//	🔴 дисциплина не определена ИЛИ нет договора с ОО ИЛИ педагог не трудоустроен;
//	🟡 дисциплина есть и трудоустройство есть, но нет индивидуального плана ИЛИ приказа о допуске;
//	🟢 полный пакет: договор, ТД, утверждённый план, приказ, расписание.
//
// Вид 2 (стажировка и практика):
//
//	🔴 срочный ТД не заключён ИЛИ наставник не назначен ИЛИ нет договора с ОО;
//	🟡 студент трудоустроен и наставник есть, но нет приказа о наставнике ИЛИ справок;
//	🟢 все документы: договор с ОО, срочный ТД, приказ о наставнике, обе справки.

// teacherPayload — полный комплект данных преподавателя из зелёной зоны ТЗ.
func teacherPayload() map[string]interface{} {
	return map[string]interface{}{
		"course_name":        "Архитектура информационных систем",
		"okz_code":           "2512",
		"it_experience_days": 365,
		"class_schedule":     "Пн 10:00, ауд. 401",
	}
}

func teacherDocuments() []string {
	return []string{"employment_contract", "appointment_order", "individual_plan"}
}

func without(values []string, drop string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != drop {
			out = append(out, value)
		}
	}
	return out
}

func TestRiskMatrixTeachers(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]interface{})
		documents []string
		want      string
	}{
		{"полный пакет — зелёная зона", nil, teacherDocuments(), "green"},
		{"дисциплина не определена — красная зона",
			func(p map[string]interface{}) { delete(p, "course_name") }, teacherDocuments(), "red"},
		{"педагог не трудоустроен: нет договора — красная зона",
			nil, without(teacherDocuments(), "employment_contract"), "red"},
		{"ИТ-стаж меньше года — красная зона",
			func(p map[string]interface{}) { p["it_experience_days"] = 364 }, teacherDocuments(), "red"},
		{"не подтверждён код ОКЗ — красная зона",
			func(p map[string]interface{}) { delete(p, "okz_code") }, teacherDocuments(), "red"},
		{"нет приказа о допуске — жёлтая зона",
			nil, without(teacherDocuments(), "appointment_order"), "yellow"},
		{"нет индивидуального плана — жёлтая зона",
			nil, without(teacherDocuments(), "individual_plan"), "yellow"},
		{"нет расписания — жёлтая зона",
			func(p map[string]interface{}) { delete(p, "class_schedule") }, teacherDocuments(), "yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := teacherPayload()
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			got := Evaluate("teachers", "fact", payload, tc.documents)
			if got.State != tc.want {
				t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
			}
			if tc.want == "green" && (!got.Ready || !got.Eligible) {
				t.Fatalf("зелёная зона должна быть готова и допустима: %+v", got)
			}
			if tc.want == "red" && got.Eligible {
				t.Fatalf("красная зона не может быть допустимой: %+v", got)
			}
		})
	}
}

func internshipPayload() map[string]interface{} {
	return map[string]interface{}{"mentor_id": "mentor-1", "student_full_name": "Архипов Д.С."}
}

func internshipDocuments() []string {
	return []string{"internship_agreement", "mentor_order", "individual_program", "incoming_certificate", "outgoing_certificate"}
}

func TestRiskMatrixInternship(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]interface{})
		documents []string
		want      string
	}{
		{"все документы загружены — зелёная зона", nil, internshipDocuments(), "green"},
		{"наставник не назначен — красная зона",
			func(p map[string]interface{}) { delete(p, "mentor_id") }, internshipDocuments(), "red"},
		{"нет основания стажировки — красная зона",
			nil, without(internshipDocuments(), "internship_agreement"), "red"},
		{"нет приказа о наставнике — жёлтая зона",
			nil, without(internshipDocuments(), "mentor_order"), "yellow"},
		{"нет входящей справки — жёлтая зона",
			nil, without(internshipDocuments(), "incoming_certificate"), "yellow"},
		{"нет итоговой справки — жёлтая зона",
			nil, without(internshipDocuments(), "outgoing_certificate"), "yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := internshipPayload()
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			got := Evaluate("internship", "fact", payload, tc.documents)
			if got.State != tc.want {
				t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
			}
		})
	}
}

func practicePayload() map[string]interface{} {
	return map[string]interface{}{
		"mentor_id": "mentor-1", "labor_contract_type": "fixed_term", "labor_contract_number": "88-ТД",
	}
}

func practiceDocuments() []string {
	return []string{"labor_contract", "practice_agreement", "mentor_order", "individual_program", "outgoing_certificate"}
}

func TestRiskMatrixEmploymentPractice(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(map[string]interface{})
		documents []string
		want      string
	}{
		{"все документы загружены — зелёная зона", nil, practiceDocuments(), "green"},
		{"срочный ТД не заключён — красная зона",
			func(p map[string]interface{}) { p["labor_contract_type"] = "other" }, practiceDocuments(), "red"},
		{"нет скана трудового договора — красная зона",
			nil, without(practiceDocuments(), "labor_contract"), "red"},
		{"нет договора о практической подготовке — красная зона",
			nil, without(practiceDocuments(), "practice_agreement"), "red"},
		{"наставник не назначен — красная зона",
			func(p map[string]interface{}) { delete(p, "mentor_id") }, practiceDocuments(), "red"},
		{"нет приказа о наставнике — жёлтая зона",
			nil, without(practiceDocuments(), "mentor_order"), "yellow"},
		{"нет итоговых документов — жёлтая зона",
			nil, without(practiceDocuments(), "outgoing_certificate"), "yellow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := practicePayload()
			if tc.mutate != nil {
				tc.mutate(payload)
			}
			got := Evaluate("employment_practice", "fact", payload, tc.documents)
			if got.State != tc.want {
				t.Fatalf("зона %q, ожидалась %q (blocking=%v, warnings=%v)", got.State, tc.want, got.Blocking, got.Warnings)
			}
		})
	}
}

// SCH-05: без периода, участников и контрольной суммы выгрузки цифровой след
// нечем сверить, поэтому готовность Вида 8 блокируется.
func TestDigitalTraceManifestBlocksReadiness(t *testing.T) {
	full := map[string]interface{}{
		"digital_trace_period_start": "2026-01-01", "digital_trace_period_end": "2026-05-01",
		"digital_trace_participants": 45,
		"digital_trace_sha256":       "9f2c1f8b7d6e5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
	}
	documents := []string{"school_agreement", "digital_trace", "acceptance_act"}
	if got := Evaluate("edu_content", "fact", full, documents); got.State != "green" {
		t.Fatalf("полный комплект должен быть зелёным: %+v", got)
	}
	for _, key := range []string{"digital_trace_period_start", "digital_trace_period_end", "digital_trace_participants", "digital_trace_sha256"} {
		payload := map[string]interface{}{}
		for k, v := range full {
			payload[k] = v
		}
		delete(payload, key)
		got := Evaluate("edu_content", "fact", payload, documents)
		if got.State != "red" || got.Ready {
			t.Fatalf("без %q цифровой след не подтверждён, ожидалась красная зона: %+v", key, got)
		}
	}
}
