package handlers

import (
	"encoding/json"
	"testing"
)

func TestAnnex4ActivityMetrics(t *testing.T) {
	tests := []struct {
		category string
		payload  string
		volume   float64
		reach    float64
		unit     string
	}{
		{"teachers", `{"academic_hours":72,"students_reach":30}`, 72, 30, "академический час"},
		{"internship", `{"duration_months":2,"student_load_hours_per_month":80}`, 160, 1, "человеко-час"},
		{"edu_content", `{"student_platform_months":100,"teacher_platform_months":20,"participants":20}`, 120, 20, "человеко-месяц"},
	}
	for _, test := range tests {
		volume, reach, unit := activityMetrics(test.category, numericPayload(json.RawMessage(test.payload)))
		if volume != test.volume || reach != test.reach || unit != test.unit {
			t.Fatalf("%s: got volume=%v reach=%v unit=%q", test.category, volume, reach, unit)
		}
	}
}
