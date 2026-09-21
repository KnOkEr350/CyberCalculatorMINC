// Package domain contains teaching workload rules defined by ТЗ 4.4.
package domain

import "fmt"

// EducationLevel is the normalized education programme level used by teaching records.
type EducationLevel string

const (
	EducationLevelBachelor   EducationLevel = "bachelor"
	EducationLevelMaster     EducationLevel = "master"
	EducationLevelSpecialist EducationLevel = "specialist"
	EducationLevelSPO        EducationLevel = "spo"
)

// SemesterRange is an inclusive semester interval.
type SemesterRange struct {
	First int
	Last  int
}

var semesterRanges = map[EducationLevel]SemesterRange{
	EducationLevelBachelor:   {First: 1, Last: 8},
	EducationLevelMaster:     {First: 9, Last: 12},
	EducationLevelSpecialist: {First: 1, Last: 13},
	EducationLevelSPO:        {First: 1, Last: 10},
}

// ValidateSemester enforces the strict semester matrix from ТЗ 4.4.
func ValidateSemester(level EducationLevel, semester int) error {
	allowed, ok := semesterRanges[level]
	if !ok {
		return fmt.Errorf("неизвестный уровень образовательной программы %q", level)
	}
	if semester < allowed.First || semester > allowed.Last {
		return fmt.Errorf("для уровня %q допустимы семестры %d–%d", level, allowed.First, allowed.Last)
	}
	return nil
}
