package calculators

import (
	"math"
	"testing"

	"cybercalc/internal/models"
)

func TestOrderFormulas(t *testing.T) {
	tests := []struct {
		name     string
		category string
		audience models.Audience
		payload  map[string]interface{}
		want     float64
	}{
		{
			name:     "teachers VO",
			category: "teachers",
			audience: models.AudienceVuz,
			payload:  map[string]interface{}{"academic_hours": 10.0},
			want:     41400,
		},
		{
			name:     "teachers SPO",
			category: "teachers",
			audience: models.AudienceKolledj,
			payload:  map[string]interface{}{"academic_hours": 10.0},
			want:     39000,
		},
		{
			name:     "OOP development VO",
			category: "ood_rpd",
			audience: models.AudienceVuz,
			payload: map[string]interface{}{
				"doc_type":      "oop",
				"level":         "vo",
				"activity_type": "development",
			},
			want: 2039850,
		},
		{
			name:     "internship",
			category: "internship",
			audience: models.AudienceVuz,
			payload: map[string]interface{}{
				"student_load_hours_per_month": 20.0,
				"mentor_load_hours_per_month":  10.0,
				"duration_months":              2.0,
			},
			want: 79800,
		},
		{
			name:     "TOP IT uses cofinancing report amount",
			category: "top_it",
			audience: models.AudienceVuz,
			payload:  map[string]interface{}{"cofinancing_amount_rub": 123456.78},
			want:     123456.78,
		},
		{
			name:     "school programs",
			category: "it_clubs",
			audience: models.AudienceSchool,
			payload: map[string]interface{}{
				"academic_hours":           10.0,
				"developed_programs_count": 2.0,
			},
			want: 1104380,
		},
		{
			name:     "teacher training",
			category: "teacher_training",
			audience: models.AudienceSchool,
			payload: map[string]interface{}{
				"developed_programs_count":   1.0,
				"academic_hours_per_teacher": 16.0,
				"trained_teachers_count":     25.0,
			},
			want: 2924570,
		},
		{
			name:     "educational content",
			category: "edu_content",
			audience: models.AudienceSchool,
			payload: map[string]interface{}{
				"student_platform_months": 100.0,
				"teacher_platform_months": 10.0,
			},
			want: 765900,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calc, err := Get(tt.category)
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.category, err)
			}
			got, err := calc.Calculate(tt.audience, tt.payload)
			if err != nil {
				t.Fatalf("Calculate(): %v", err)
			}
			if math.Abs(got-tt.want) > 0.001 {
				t.Fatalf("Calculate() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}

func TestRejectsInvalidNumbers(t *testing.T) {
	calc, err := Get("teachers")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := calc.Calculate(models.AudienceVuz, map[string]interface{}{"academic_hours": -1.0}); err == nil {
		t.Fatal("negative academic_hours must be rejected")
	}
}

func TestTeacherPayloadEnforcesSemesterMatrix(t *testing.T) {
	calc, err := Get("teachers")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]interface{}{
		"org_name":          "partner-id",
		"staff_member_id":   "staff-member-id",
		"course_name":       "Информационная безопасность",
		"teacher_full_name": "Петров Пётр Петрович",
		"employment_form":   "ГПХ",
		"academic_hours":    10.0,
	}
	tests := []struct {
		level     string
		semester  float64
		wantError bool
	}{
		{level: "bachelor", semester: 1},
		{level: "bachelor", semester: 8},
		{level: "bachelor", semester: 9, wantError: true},
		{level: "master", semester: 8, wantError: true},
		{level: "master", semester: 9},
		{level: "master", semester: 12},
		{level: "specialist", semester: 13},
		{level: "specialist", semester: 14, wantError: true},
		{level: "spo", semester: 10},
		{level: "spo", semester: 11, wantError: true},
	}
	for _, test := range tests {
		payload := make(map[string]interface{}, len(base)+2)
		for key, value := range base {
			payload[key] = value
		}
		payload["education_level"] = test.level
		payload["semester"] = test.semester
		err := ValidatePayload(calc, payload)
		if (err != nil) != test.wantError {
			t.Fatalf("level=%s semester=%v error=%v, wantError=%v", test.level, test.semester, err, test.wantError)
		}
	}
}

func TestValidatePayloadRequiresTextAndValidSelect(t *testing.T) {
	calc, err := Get("minc_decision")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"org_name":             "partner-id",
		"decision_reference":   " ",
		"activity_description": "Мероприятие",
		"metric_description":   "Метрика",
		"calculation_basis":    "Методика",
		"amount_manual":        1000.0,
	}
	if err := ValidatePayload(calc, payload); err == nil {
		t.Fatal("blank required text must be rejected")
	}

	ood, err := Get("ood_rpd")
	if err != nil {
		t.Fatal(err)
	}
	invalidSelect := map[string]interface{}{
		"org_name":      "partner-id",
		"doc_type":      "invalid",
		"level":         "vo",
		"activity_type": "development",
		"program_name":  "Программа",
	}
	if err := ValidatePayload(ood, invalidSelect); err == nil {
		t.Fatal("unknown select option must be rejected")
	}
}

func TestValidatePayloadRejectsUnexpectedHugeAndFractionalValues(t *testing.T) {
	school, err := Get("it_clubs")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]interface{}{
		"org_name":                 "partner-id",
		"program_name":             "Программа",
		"academic_hours":           10.0,
		"developed_programs_count": 1.0,
		"students_count":           20.0,
		"funding_source":           "Средства ИТ-компании",
		"budget_funding":           "absent",
		"citizen_funding":          "absent",
	}

	withUnexpected := make(map[string]interface{}, len(base)+1)
	for key, value := range base {
		withUnexpected[key] = value
	}
	withUnexpected["unexpected"] = "value"
	if err := ValidatePayload(school, withUnexpected); err == nil {
		t.Fatal("unexpected payload field must be rejected")
	}

	base["students_count"] = 1.5
	if err := ValidatePayload(school, base); err == nil {
		t.Fatal("fractional people count must be rejected")
	}

	base["students_count"] = 20.0
	base["academic_hours"] = 1_000_000_000_001.0
	if err := ValidatePayload(school, base); err == nil {
		t.Fatal("huge payload number must be rejected")
	}
}

func TestSchoolActivitiesRejectProhibitedFunding(t *testing.T) {
	tests := []struct {
		category string
		payload  map[string]interface{}
	}{
		{
			category: "it_clubs",
			payload: map[string]interface{}{
				"org_name": "school-id", "program_name": "ИТ-кружок", "academic_hours": 72.0,
				"developed_programs_count": 1.0, "students_count": 25.0,
			},
		},
		{
			category: "teacher_training",
			payload: map[string]interface{}{
				"org_name": "school-id", "program_name": "Повышение квалификации", "developed_programs_count": 1.0,
				"academic_hours_per_teacher": 16.0, "trained_teachers_count": 20.0,
			},
		},
		{
			category: "edu_content",
			payload: map[string]interface{}{
				"org_name": "school-id", "platform_name": "Моя школа", "student_platform_months": 100.0,
				"teacher_platform_months": 10.0, "digital_trace_reference": "Логи от 01.09.2026",
				// SCH-05: выгрузка цифрового следа подтверждается периодом,
				// числом участников и контрольной суммой файла.
				"digital_trace_period_start": "2026-01-01", "digital_trace_period_end": "2026-05-01",
				"digital_trace_participants": 110.0,
				"digital_trace_sha256":       "9f2c1f8b7d6e5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.category, func(t *testing.T) {
			calc, err := Get(test.category)
			if err != nil {
				t.Fatal(err)
			}
			base := make(map[string]interface{}, len(test.payload)+3)
			for key, value := range test.payload {
				base[key] = value
			}
			base["funding_source"] = "100% средства ИТ-компании"
			base["budget_funding"] = "absent"
			base["citizen_funding"] = "absent"
			if err := ValidatePayload(calc, base); err != nil {
				t.Fatalf("eligible funding rejected: %v", err)
			}

			for _, prohibited := range []string{"budget_funding", "citizen_funding"} {
				payload := make(map[string]interface{}, len(base))
				for key, value := range base {
					payload[key] = value
				}
				payload[prohibited] = "full_or_partial"
				if err := ValidatePayload(calc, payload); err == nil {
					t.Fatalf("%s must block %s", prohibited, test.category)
				}
			}
		})
	}
}

func TestEmploymentPracticeRequiresFixedTermLaborContract(t *testing.T) {
	calc, err := Get("employment_practice")
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string]interface{}{
		"org_name": "partner-id", "mentor_id": "mentor-id", "mentor_full_name": "Иванов Иван Иванович",
		"student_full_name": "Петров Пётр Петрович", "duration_months": 2.0,
		"student_load_hours_per_month": 10.0, "mentor_load_hours_per_month": 3.0,
		"labor_contract_type": "fixed_term", "labor_contract_number": "ТД-42", "labor_contract_date": "2026-09-01",
		// PRA-04: практика подтверждается нормами ТК РФ, поэтому возраст и
		// недельные часы обязательны.
		"student_age": 19.0, "weekly_hours": 30.0,
	}
	if err := ValidatePayload(calc, valid); err != nil {
		t.Fatalf("valid fixed-term contract rejected: %v", err)
	}

	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{name: "missing number", key: "labor_contract_number", value: nil},
		{name: "invalid date", key: "labor_contract_date", value: "2026-02-30"},
		{name: "non fixed-term contract", key: "labor_contract_type", value: "other"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := make(map[string]interface{}, len(valid))
			for key, value := range valid {
				payload[key] = value
			}
			if test.value == nil {
				delete(payload, test.key)
			} else {
				payload[test.key] = test.value
			}
			if err := ValidatePayload(calc, payload); err == nil {
				t.Fatal("invalid employment practice was accepted")
			}
		})
	}

	internship, err := Get("internship")
	if err != nil {
		t.Fatal(err)
	}
	// У обычной стажировки нет ни реквизитов трудового договора, ни полей
	// подтверждения норм ТК РФ: они относятся только к практике.
	practiceOnly := map[string]bool{
		"labor_contract_type": true, "labor_contract_number": true, "labor_contract_date": true,
		"student_age": true, "weekly_hours": true,
	}
	withoutContract := make(map[string]interface{}, len(valid)-len(practiceOnly))
	for key, value := range valid {
		if !practiceOnly[key] {
			withoutContract[key] = value
		}
	}
	if err := ValidatePayload(internship, withoutContract); err != nil {
		t.Fatalf("internship unexpectedly requires practice contract fields: %v", err)
	}
}

func TestValidatePayloadNormalizesTextAndAmountBounds(t *testing.T) {
	calc, err := Get("top_it")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"org_name":                     " partner-id ",
		"project_name":                 " Проект ",
		"program_name":                 " Программа ",
		"cofinancing_report_reference": " Отчёт № 1 ",
		"cofinancing_amount_rub":       1000.0,
	}
	if err := ValidatePayload(calc, payload); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	if payload["project_name"] != "Проект" || payload["org_name"] != "partner-id" {
		t.Fatalf("text was not normalized: %#v", payload)
	}
	if err := ValidateAmount(-1); err == nil {
		t.Fatal("negative calculated amount must be rejected")
	}
	if err := ValidateAmount(1000); err != nil {
		t.Fatalf("valid calculated amount rejected: %v", err)
	}
}
