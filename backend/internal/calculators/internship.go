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
		{Key: "period_start", Label: "Дата начала", Type: "date"},
		{Key: "period_end", Label: "Дата окончания", Type: "date"},
		{Key: "specialty_code", Label: "Код ИТ-специальности по приказу № 27", Type: "text"},
		{Key: "duration_months", Label: "Продолжительность, мес.", Type: "number", Required: true},
		{Key: "student_load_hours_per_month", Label: "Нагрузка студента, ч/мес", Type: "number", Required: true},
		{Key: "mentor_load_hours_per_month", Label: "Нагрузка наставника, ч/мес", Type: "number", Required: true},
		{Key: "monthly_salary_rub", Label: "Заработная плата студента в месяц, руб.", Type: "number", Minimum: 0},
		{Key: "internship_agreement_reference", Label: "Реквизиты договора о стажировке", Type: "text", MaxLength: 1000},
		{Key: "labor_contract_number", Label: "Номер трудового договора", Type: "text", MaxLength: 100},
		{Key: "labor_contract_date", Label: "Дата трудового договора", Type: "date"},
		{Key: "mentor_order_reference", Label: "Реквизиты приказа о наставнике", Type: "text", MaxLength: 1000},
		{Key: "individual_program_reference", Label: "Индивидуальная программа / табель", Type: "text", MaxLength: 1000},
		{Key: "incoming_certificate_reference", Label: "Входящая справка", Type: "text", MaxLength: 1000},
		{Key: "outgoing_certificate_reference", Label: "Итоговая справка", Type: "text", MaxLength: 1000},
	}
}

func (employmentPracticeCalc) Fields() []FieldSpec {
	return append(internshipCalc{}.Fields(),
		FieldSpec{Key: "labor_contract_type", Label: "Тип трудового договора", Type: "select", Required: true, Options: []string{"fixed_term", "other"}},
		FieldSpec{Key: "practice_agreement_reference", Label: "Реквизиты договора о практической подготовке", Type: "text", MaxLength: 1000},
		FieldSpec{Key: "student_age", Label: "Возраст практиканта", Type: "number", Integer: true, Minimum: 14, Maximum: 100},
		FieldSpec{Key: "weekly_hours", Label: "Рабочих часов в неделю", Type: "number", Minimum: 0, Maximum: 40},
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
	if _, err := str(payload, "labor_contract_number"); err != nil {
		return err
	}
	if _, err := str(payload, "labor_contract_date"); err != nil {
		return err
	}
	_, ageOK := payload["student_age"]
	_, hoursOK := payload["weekly_hours"]
	if ageOK && hoursOK {
		a, err := num(payload, "student_age")
		if err != nil {
			return err
		}
		h, err := num(payload, "weekly_hours")
		if err != nil {
			return err
		}
		limit := 40.0
		if a < 16 {
			limit = 24
		} else if a < 18 {
			limit = 35
		}
		if h > limit {
			return fmt.Errorf("рабочее время %.1f ч/нед. превышает предел %.0f ч для указанного возраста", h, limit)
		}
	}
	return nil
}
