package domain

import "testing"

func TestValidateSemesterMatrix(t *testing.T) {
	tests := []struct {
		name      string
		level     EducationLevel
		semester  int
		wantError bool
	}{
		{name: "bachelor first", level: EducationLevelBachelor, semester: 1},
		{name: "bachelor last", level: EducationLevelBachelor, semester: 8},
		{name: "bachelor above", level: EducationLevelBachelor, semester: 9, wantError: true},
		{name: "master below", level: EducationLevelMaster, semester: 8, wantError: true},
		{name: "master first", level: EducationLevelMaster, semester: 9},
		{name: "master last", level: EducationLevelMaster, semester: 12},
		{name: "specialist last", level: EducationLevelSpecialist, semester: 13},
		{name: "specialist above", level: EducationLevelSpecialist, semester: 14, wantError: true},
		{name: "SPO last", level: EducationLevelSPO, semester: 10},
		{name: "SPO above", level: EducationLevelSPO, semester: 11, wantError: true},
		{name: "zero", level: EducationLevelBachelor, semester: 0, wantError: true},
		{name: "unknown level", level: EducationLevel("unknown"), semester: 1, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateSemester(test.level, test.semester)
			if (err != nil) != test.wantError {
				t.Fatalf("ValidateSemester(%q, %d) error = %v, wantError %v", test.level, test.semester, err, test.wantError)
			}
		})
	}
}
