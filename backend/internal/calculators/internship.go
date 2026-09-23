package calculators

import (
	"fmt"
	"math"

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
		FieldSpec{Key: "practice_agreement_number", Label: "Номер договора о практической подготовке", Type: "text", Required: true, MaxLength: 100},
		FieldSpec{Key: "practice_agreement_date", Label: "Дата договора о практической подготовке", Type: "date", Required: true},
		FieldSpec{Key: "practice_agreement_start_date", Label: "Дата начала действия договора о практической подготовке", Type: "date"},
		FieldSpec{Key: "practice_agreement_end_date", Label: "Дата окончания действия договора о практической подготовке", Type: "date"},
		FieldSpec{Key: "practice_agreement_reference", Label: "Дополнительные реквизиты договора о практической подготовке", Type: "text", MaxLength: 1000},
		// Возраст и недельные часы обязательны: без них нечем подтвердить
		// соблюдение статей 63 и 92 ТК РФ, а практика без такого
		// подтверждения к зачёту не принимается (ТЗ, п. 7.3).
		FieldSpec{Key: "student_age", Label: "Возраст практиканта", Type: "number", Required: true, Integer: true, Minimum: 14, Maximum: 100},
		FieldSpec{Key: "weekly_hours", Label: "Рабочих часов в неделю", Type: "number", Required: true, Minimum: 0, Maximum: 40},
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
	if _, err := str(payload, "practice_agreement_number"); err != nil {
		return err
	}
	if _, err := str(payload, "practice_agreement_date"); err != nil {
		return err
	}
	age, err := num(payload, "student_age")
	if err != nil {
		return fmt.Errorf("укажите возраст практиканта: без него не проверить нормы ТК РФ")
	}
	hours, err := num(payload, "weekly_hours")
	if err != nil {
		return fmt.Errorf("укажите рабочих часов в неделю: без них не проверить нормы ТК РФ")
	}
	return validateWorkingTimeLimit(age, hours)
}

// minimumEmploymentAge — трудовой договор с обучающимся заключается не
// ранее 14 лет (статья 63 ТК РФ).
const minimumEmploymentAge = 14

// weeklyHoursLimit — предельная продолжительность рабочего времени в неделю
// по статье 92 ТК РФ: до 16 лет — 24 часа, от 16 до 18 лет — 35 часов.
// Для совершеннолетних действует общая норма статьи 91 — 40 часов.
func weeklyHoursLimit(age float64) float64 {
	switch {
	case age < 16:
		return 24
	case age < 18:
		return 35
	default:
		return 40
	}
}

// validateWorkingTimeLimit — жёсткая комплаенс-блокировка ТК РФ для практики
// с трудоустройством (PRA-04).
func validateWorkingTimeLimit(age, hours float64) error {
	if math.IsNaN(age) || math.IsInf(age, 0) || age != math.Trunc(age) {
		return fmt.Errorf("возраст практиканта должен быть целым числом лет")
	}
	if age < minimumEmploymentAge {
		return fmt.Errorf("трудовой договор с практикантом младше %d лет не допускается статьёй 63 ТК РФ", minimumEmploymentAge)
	}
	if math.IsNaN(hours) || math.IsInf(hours, 0) || hours <= 0 {
		return fmt.Errorf("рабочее время практиканта должно быть больше нуля")
	}
	if limit := weeklyHoursLimit(age); hours > limit {
		return fmt.Errorf("рабочее время %g ч/нед. превышает предел %g ч, установленный ТК РФ для возраста %g лет", hours, limit, age)
	}
	return nil
}
