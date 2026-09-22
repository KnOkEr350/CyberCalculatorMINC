package handlers

import (
	"strings"
	"testing"
)

func TestNormalizeOrgUnit(t *testing.T) {
	req := orgUnitWriteRequest{
		UnitLevelType: "department",
		UnitName:      "  Кафедра   информационной безопасности ",
		HeadFIO:       " Иванов   Иван Иванович ",
	}
	if err := normalizeOrgUnit(&req); err != nil {
		t.Fatal(err)
	}
	if req.UnitName != "Кафедра информационной безопасности" || req.HeadFIO != "Иванов Иван Иванович" {
		t.Fatalf("fields were not normalized: %+v", req)
	}

	req.UnitLevelType = "unknown"
	if err := normalizeOrgUnit(&req); err == nil {
		t.Fatal("unknown unit type must be rejected")
	}
	req.UnitLevelType = "faculty"
	req.UnitName = strings.Repeat("я", 301)
	if err := normalizeOrgUnit(&req); err == nil {
		t.Fatal("oversized unit name must be rejected")
	}
}

func TestNormalizeAcademicGroupSemesterMatrix(t *testing.T) {
	base := academicGroupWriteRequest{
		UnitID: "00000000-0000-0000-0000-000000000001", GroupName: "БПИ-231",
		EducationLevel: "vo_bachelor", CourseNum: 3, CurrentSemester: 5,
		SemesterPeriod: "autumn", SpecialtyCode: "09.03.01", StudentsCount: 30,
	}
	if err := normalizeAcademicGroup(&base); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		level     string
		semester  int
		wantError bool
	}{
		{"bachelor lower", "vo_bachelor", 1, false},
		{"bachelor upper", "vo_bachelor", 8, false},
		{"bachelor overflow", "vo_bachelor", 9, true},
		{"master lower", "vo_master", 9, false},
		{"master upper", "vo_master", 12, false},
		{"master legacy semester", "vo_master", 2, true},
		{"specialist upper", "vo_specialist", 13, false},
		{"spo upper", "spo", 10, false},
		{"spo overflow", "spo", 11, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := base
			req.EducationLevel = test.level
			req.CurrentSemester = test.semester
			err := normalizeAcademicGroup(&req)
			if (err != nil) != test.wantError {
				t.Fatalf("normalizeAcademicGroup() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestNormalizeAcademicGroupRejectsInvalidFields(t *testing.T) {
	req := academicGroupWriteRequest{
		UnitID: "unit", GroupName: "Группа", EducationLevel: "spo", CourseNum: 1,
		CurrentSemester: 1, SemesterPeriod: "autumn", SpecialtyCode: "9.03.01", StudentsCount: 1,
	}
	if err := normalizeAcademicGroup(&req); err == nil {
		t.Fatal("malformed specialty code must be rejected")
	}
	req.SpecialtyCode = "09.03.01"
	req.StudentsCount = -1
	if err := normalizeAcademicGroup(&req); err == nil {
		t.Fatal("negative student count must be rejected")
	}
}
