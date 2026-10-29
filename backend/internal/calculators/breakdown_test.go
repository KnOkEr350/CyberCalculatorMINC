package calculators

import (
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"fmt"
	"strings"
	"testing"
)

func TestBreakdownMatchesExactCalculation(t *testing.T) {
	cases := []struct {
		code     string
		audience models.Audience
		payload  map[string]interface{}
		excel    string
	}{
		{"teachers", models.AudienceVuz, map[string]interface{}{"academic_hours": 72.5}, "ROUND(A1*A2,2)"},
		{"teachers", models.AudienceKolledj, map[string]interface{}{"academic_hours": 48.0}, "ROUND(A1*A2,2)"},
		{"internship", models.AudienceVuz, map[string]interface{}{"student_load_hours_per_month": 80.0, "mentor_load_hours_per_month": 10.5, "duration_months": 2.5}, "ROUND((A1*A2+A3*A4)*A5,2)"},
		{"employment_practice", models.AudienceKolledj, map[string]interface{}{"student_load_hours_per_month": 33.3, "mentor_load_hours_per_month": 0.0, "duration_months": 1.0}, "ROUND((A1*A2+A3*A4)*A5,2)"},
		{"ood_rpd", models.AudienceVuz, map[string]interface{}{"doc_type": "oop", "level": "vo", "activity_type": "update"}, "ROUND(A1,2)"},
		{"top_it", models.AudienceVuz, map[string]interface{}{"cofinancing_amount_rub": 1000.0, "actual_spent_amount_rub": 750.55}, "ROUND(A1,2)"},
		{"minc_decision", models.AudienceVuz, map[string]interface{}{"amount_manual": 800000.0}, "ROUND(A1,2)"},
		{"it_clubs", models.AudienceSchool, map[string]interface{}{"academic_hours": 68.0, "developed_programs_count": 1.0}, "ROUND(A1*A2+A3*A4,2)"},
		{"teacher_training", models.AudienceSchool, map[string]interface{}{"developed_programs_count": 1.0, "academic_hours_per_teacher": 36.0, "trained_teachers_count": 15.0}, "ROUND(A1*A2+A3*A4*A5,2)"},
		{"edu_content", models.AudienceSchool, map[string]interface{}{"student_platform_months": 120.0, "teacher_platform_months": 12.0}, "ROUND(A1*A2+A3*A4,2)"},
	}
	card := tariffs.Default()
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_%s", c.code, c.audience), func(t *testing.T) {
			want, err := CalculateAmountWith(card, c.code, c.audience, c.payload)
			if err != nil {
				t.Fatal(err)
			}
			b, err := BreakdownWith(card, c.code, c.audience, c.payload)
			if err != nil {
				t.Fatal(err)
			}
			got, err := money.FromRat(b.Eval())
			if err != nil {
				t.Fatal(err)
			}
			if got != want || b.Amount != want {
				t.Fatalf("breakdown %s / %s, exact %s", got, b.Amount, want)
			}
			excel := b.Excel(func(i int) string { return fmt.Sprintf("A%d", i+1) })
			if excel != c.excel {
				t.Fatalf("formula %q, want %q", excel, c.excel)
			}
			if strings.Contains(b.Describe(), "\x00") || !strings.Contains(b.Describe(), "[") {
				t.Fatalf("description %q", b.Describe())
			}
			for _, input := range b.Inputs {
				if input.Label == "" || input.Value == nil {
					t.Fatalf("input without label or value: %+v", input)
				}
			}
		})
	}
}

func TestBreakdownRejectsMissingNumbers(t *testing.T) {
	if _, err := BreakdownWith(tariffs.Default(), "teachers", models.AudienceVuz, map[string]interface{}{}); err == nil {
		t.Fatal("missing hours accepted")
	}
}
