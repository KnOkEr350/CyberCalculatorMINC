package calculators

import (
	"strings"
	"testing"
)

// SCH-05: логи цифрового следа ФГИС «Моя школа» подтверждаются периодом
// выгрузки, числом участников и контрольной суммой файла. По одной текстовой
// ссылке проверить выгрузку нечем.
func digitalTracePayload() map[string]interface{} {
	return map[string]interface{}{
		"org_name": "school-id", "platform_name": "Моя школа",
		"student_platform_months": 100.0, "teacher_platform_months": 10.0,
		"digital_trace_reference":    "Выгрузка логов от 01.09.2026",
		"digital_trace_period_start": "2026-01-01",
		"digital_trace_period_end":   "2026-05-01",
		"digital_trace_participants": 110.0,
		"digital_trace_sha256":       "9f2c1f8b7d6e5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
		"funding_source":             "100% средства ИТ-компании",
		"budget_funding":             "absent",
		"citizen_funding":            "absent",
	}
}

func TestDigitalTraceManifestValidation(t *testing.T) {
	calc, err := Get("edu_content")
	if err != nil {
		t.Fatal(err)
	}
	validator, ok := calc.(payloadValidator)
	if !ok {
		t.Fatal("для цифрового контента должен быть валидатор")
	}
	if err := validator.Validate(digitalTracePayload()); err != nil {
		t.Fatalf("корректная выгрузка должна проходить: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(map[string]interface{})
		wantErr string
	}{
		{"нет начала периода", func(p map[string]interface{}) { delete(p, "digital_trace_period_start") }, "начало периода"},
		{"нет конца периода", func(p map[string]interface{}) { delete(p, "digital_trace_period_end") }, "конец периода"},
		{"период задом наперёд", func(p map[string]interface{}) {
			p["digital_trace_period_start"] = "2026-05-01"
			p["digital_trace_period_end"] = "2026-01-01"
		}, "заканчивается раньше"},
		{"нечитаемая дата", func(p map[string]interface{}) { p["digital_trace_period_start"] = "01.01.2026" }, "ГГГГ-ММ-ДД"},
		{"нет участников", func(p map[string]interface{}) { delete(p, "digital_trace_participants") }, "число участников"},
		{"ноль участников", func(p map[string]interface{}) { p["digital_trace_participants"] = 0.0 }, "число участников"},
		{"дробное число участников", func(p map[string]interface{}) { p["digital_trace_participants"] = 10.5 }, "целым"},
		{"нет контрольной суммы", func(p map[string]interface{}) { delete(p, "digital_trace_sha256") }, "контрольную сумму"},
		{"короткая контрольная сумма", func(p map[string]interface{}) { p["digital_trace_sha256"] = "abc123" }, "SHA-256"},
		{"не шестнадцатеричная сумма", func(p map[string]interface{}) {
			p["digital_trace_sha256"] = strings.Repeat("z", 64)
		}, "SHA-256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := digitalTracePayload()
			tc.mutate(payload)
			err := validator.Validate(payload)
			if err == nil {
				t.Fatalf("ожидали ошибку %q, запись принята", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ошибка %q не содержит %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// Период выгрузки в один день допустим: логи могут быть выгружены за сутки.
func TestDigitalTraceSingleDayPeriodAllowed(t *testing.T) {
	calc, _ := Get("edu_content")
	validator := calc.(payloadValidator)
	payload := digitalTracePayload()
	payload["digital_trace_period_start"] = "2026-05-01"
	payload["digital_trace_period_end"] = "2026-05-01"
	if err := validator.Validate(payload); err != nil {
		t.Fatalf("выгрузка за один день должна приниматься: %v", err)
	}
}

// Реквизиты выгрузки должны быть помечены обязательными в описании формы,
// иначе UI и XLSX-импорт не потребуют их заполнения.
func TestDigitalTraceFieldsAreRequired(t *testing.T) {
	calc, err := Get("edu_content")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	for _, field := range calc.Fields() {
		required[field.Key] = field.Required
	}
	for _, key := range []string{
		"digital_trace_period_start", "digital_trace_period_end",
		"digital_trace_participants", "digital_trace_sha256",
	} {
		if !required[key] {
			t.Fatalf("поле %q должно быть обязательным для цифрового контента", key)
		}
	}
}
