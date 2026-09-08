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
