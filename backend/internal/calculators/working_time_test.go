package calculators

import (
	"encoding/json"
	"strings"
	"testing"
)

// PRA-04: практика с трудоустройством проверяется по ТК РФ — статья 63
// (минимальный возраст) и статья 92 (сокращённая продолжительность рабочего
// времени несовершеннолетних). Для совершеннолетних действует общая норма
// статьи 91 — 40 часов.
func TestWeeklyHoursLimitByAge(t *testing.T) {
	cases := map[float64]float64{
		14: 24, 15: 24, // до 16 лет — не более 24 часов
		16: 35, 17: 35, // от 16 до 18 лет — не более 35 часов
		18: 40, 25: 40, // совершеннолетние — общая норма 40 часов
	}
	for age, want := range cases {
		if got := weeklyHoursLimit(age); got != want {
			t.Fatalf("возраст %g: предел %g ч, ожидалось %g ч", age, got, want)
		}
	}
}

func TestValidateWorkingTimeLimitBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		age, hours float64
		wantErr    string
	}{
		{"15 лет, ровно предел 24 ч", 15, 24, ""},
		{"15 лет, 24.5 ч — превышение", 15, 24.5, "превышает предел"},
		{"17 лет, ровно предел 35 ч", 17, 35, ""},
		{"17 лет, 36 ч — превышение", 17, 36, "превышает предел"},
		{"18 лет, 40 ч — общая норма", 18, 40, ""},
		{"18 лет, 41 ч — превышение", 18, 41, "превышает предел"},
		{"13 лет — договор не допускается", 13, 10, "статьёй 63"},
		{"нулевые часы", 18, 0, "больше нуля"},
		{"отрицательные часы", 18, -5, "больше нуля"},
		{"дробный возраст", 17.5, 30, "целым числом лет"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWorkingTimeLimit(tc.age, tc.hours)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ожидали допуск, получили ошибку: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ожидали ошибку %q, запись прошла проверку", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ошибка %q не содержит %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func practicePayload() map[string]interface{} {
	return map[string]interface{}{
		"org_name": "МГУ", "mentor_id": "m-1", "mentor_full_name": "Ильин С.В.", "student_full_name": "Громов А.М.",
		"duration_months": json.Number("2"), "student_load_hours_per_month": json.Number("120"),
		"mentor_load_hours_per_month": json.Number("30"),
		"labor_contract_type":         "fixed_term", "labor_contract_number": "88-ТД", "labor_contract_date": "2026-05-30",
		"student_age": json.Number("19"), "weekly_hours": json.Number("30"),
	}
}

// Регрессия: раньше проверка ТК РФ выполнялась только когда оба поля
// заполнены, поэтому её можно было обойти, просто не заполнив возраст или
// часы. Практика без такого подтверждения к зачёту не принимается.
func TestEmploymentPracticeRequiresWorkingTimeFacts(t *testing.T) {
	calc, err := Get("employment_practice")
	if err != nil {
		t.Fatal(err)
	}
	validator, ok := calc.(payloadValidator)
	if !ok {
		t.Fatal("для практики должен быть валидатор комплаенса")
	}
	if err := validator.Validate(practicePayload()); err != nil {
		t.Fatalf("корректная запись должна проходить: %v", err)
	}
	for _, field := range []string{"student_age", "weekly_hours"} {
		payload := practicePayload()
		delete(payload, field)
		err := validator.Validate(payload)
		if err == nil {
			t.Fatalf("без поля %q запись не должна приниматься к зачёту", field)
		}
		if !strings.Contains(err.Error(), "ТК РФ") {
			t.Fatalf("ошибка для %q должна ссылаться на ТК РФ, получено: %v", field, err)
		}
	}
	// Оба поля заполнены, но норма нарушена.
	payload := practicePayload()
	payload["student_age"] = json.Number("16")
	payload["weekly_hours"] = json.Number("40")
	if err := validator.Validate(payload); err == nil {
		t.Fatal("40 ч/нед. для 16-летнего практиканта должны блокироваться")
	}
}

// Поля возраста и часов должны быть помечены обязательными и в описании
// формы, иначе UI и XLSX-импорт не потребуют их заполнения.
func TestEmploymentPracticeMarksWorkingTimeFieldsRequired(t *testing.T) {
	calc, err := Get("employment_practice")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{}
	for _, field := range calc.Fields() {
		required[field.Key] = field.Required
	}
	for _, key := range []string{"student_age", "weekly_hours"} {
		if !required[key] {
			t.Fatalf("поле %q должно быть обязательным для практики", key)
		}
	}
	// У стажировки без трудоустройства этих требований нет.
	internship, err := Get("internship")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range internship.Fields() {
		if field.Key == "student_age" || field.Key == "weekly_hours" {
			t.Fatalf("поле %q не относится к обычной стажировке", field.Key)
		}
	}
}
