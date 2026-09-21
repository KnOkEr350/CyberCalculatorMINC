package calculators

import (
	"fmt"

	"cybercalc/internal/models"
)

// internshipCalc — раздел «Стажировки» / «Практика с трудоустройством» / «ТОП ИТ».
// Формула из ТЗ: (Нагрузка студента, ч/мес х 800 руб. + Нагрузка наставника,
// ч/мес х 2390 руб.) х Продолжительность, мес.
type internshipCalc struct{}

// employmentPracticeCalc переиспользует расчёт стажировки, но требует
// отдельное подтверждение официального трудоустройства практиканта.
type employmentPracticeCalc struct {
	internshipCalc
}

const (
	studentHourRate = 800.0
	mentorHourRate  = 2390.0
)

func (internshipCalc) Calculate(_ models.Audience, payload map[string]interface{}) (float64, error) {
	studentLoad, err := nonNegativeNum(payload, "student_load_hours_per_month")
	if err != nil {
		return 0, err
	}
	mentorLoad, err := nonNegativeNum(payload, "mentor_load_hours_per_month")
	if err != nil {
		return 0, err
	}
	duration, err := positiveNum(payload, "duration_months")
	if err != nil {
		return 0, err
	}
	if studentLoad == 0 && mentorLoad == 0 {
		return 0, fmt.Errorf("должна быть указана нагрузка студента или наставника")
	}
	amount := (studentLoad*studentHourRate + mentorLoad*mentorHourRate) * duration
	return round2(amount), nil
}

func (internshipCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select", Required: true},
		{Key: "mentor_id", Label: "Наставник из справочника", Type: "select", Required: true},
		{Key: "mentor_full_name", Label: "ФИО наставника", Type: "text", Required: true},
		{Key: "student_full_name", Label: "ФИО студента", Type: "text", Required: true},
		{Key: "course", Label: "Курс", Type: "text"},
		{Key: "period", Label: "Период проведения", Type: "text"},
		{Key: "duration_months", Label: "Продолжительность, мес.", Type: "number", Required: true},
		{Key: "student_load_hours_per_month", Label: "Нагрузка студента, ч/мес", Type: "number", Required: true},
		{Key: "mentor_load_hours_per_month", Label: "Нагрузка наставника, ч/мес", Type: "number", Required: true},
	}
}

func (employmentPracticeCalc) Fields() []FieldSpec {
	return append(internshipCalc{}.Fields(),
		FieldSpec{Key: "labor_contract_type", Label: "Тип трудового договора", Type: "select", Required: true, Options: []string{"fixed_term", "other"}},
		FieldSpec{Key: "labor_contract_number", Label: "Номер срочного трудового договора", Type: "text", Required: true, MaxLength: 100},
		FieldSpec{Key: "labor_contract_date", Label: "Дата срочного трудового договора", Type: "date", Required: true},
	)
}

func (employmentPracticeCalc) Validate(payload map[string]interface{}) error {
	contractType, err := str(payload, "labor_contract_type")
	if err != nil {
		return err
	}
	if contractType != "fixed_term" {
		return fmt.Errorf("практика принимается к зачёту только при наличии срочного трудового договора")
	}
	return nil
}
