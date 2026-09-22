package calculators

import (
	"fmt"

	"cybercalc/internal/models"
)

const (
	schoolProgramAcademicHourRate  = 4260.0
	schoolProgramDevelopmentRate   = 530890.0
	teacherTrainingHourRate        = 3790.0
	teacherTrainingDevelopmentRate = 1408570.0
	studentPlatformMonthRate       = 6800.0
	teacherPlatformMonthRate       = 8590.0
)

// topITCalc учитывает именно объём софинансирования из отчёта получателя
// гранта/образовательной организации. Формула стажировок здесь неприменима.
type topITCalc struct{}

func (topITCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	if audience != models.AudienceVuz {
		return 0, fmt.Errorf("категория «ТОП ИТ / ТОП ИИ» поддерживает только высшее образование")
	}
	amountKey := "cofinancing_amount_rub"
	if _, ok := payload["actual_spent_amount_rub"]; ok {
		amountKey = "actual_spent_amount_rub"
	}
	amount, err := positiveNum(payload, amountKey)
	if err != nil {
		return 0, err
	}
	return round2(amount), nil
}

func (topITCalc) Validate(payload map[string]interface{}) error {
	spentRaw, spentOK := payload["actual_spent_amount_rub"]
	transferredRaw, transferredOK := payload["transferred_amount_rub"]
	if !spentOK || !transferredOK || spentRaw == nil || transferredRaw == nil {
		return nil
	}
	spent, err := nonNegativeNum(payload, "actual_spent_amount_rub")
	if err != nil {
		return err
	}
	transferred, err := nonNegativeNum(payload, "transferred_amount_rub")
	if err != nil {
		return err
	}
	if spent > transferred {
		return fmt.Errorf("фактически израсходованная сумма не может превышать перечисленную")
	}
	return nil
}

func (topITCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование образовательной организации", Type: "select", Required: true},
		{Key: "project_name", Label: "Наименование федерального проекта", Type: "text", Required: true, MaxLength: 300},
		{Key: "program_name", Label: "Наименование образовательной программы топ-уровня", Type: "text", Required: true, MaxLength: 500},
		{Key: "joint_activities", Label: "Совместные активности", Type: "text", MaxLength: 2000},
		{Key: "top_activity_type", Label: "Тип активности", Type: "select", Options: []string{"assistance", "cofinancing"}},
		{Key: "duration_months", Label: "Продолжительность, мес.", Type: "number"},
		{Key: "cost_type", Label: "Тип затрат", Type: "text"},
		{Key: "cofinancing_report_reference", Label: "Реквизиты отчёта о софинансировании", Type: "text", Required: true},
		{Key: "cofinancing_amount_rub", Label: "Объём софинансирования по отчёту, руб.", Type: "number", Required: true},
		{Key: "program_wave", Label: "Волна программы", Type: "text"},
		{Key: "partner_role", Label: "Роль организации", Type: "select", Options: []string{"anchor", "partner"}},
		{Key: "grant_amount_rub", Label: "Размер гранта, руб.", Type: "number", Minimum: 0},
		{Key: "planned_cofinancing_amount_rub", Label: "План софинансирования X, руб.", Type: "number", Minimum: 0},
		{Key: "transferred_amount_rub", Label: "Перечислено, руб.", Type: "number", Minimum: 0},
		{Key: "actual_spent_amount_rub", Label: "Фактически израсходовано, руб.", Type: "number", Minimum: 0},
		{Key: "expense_article", Label: "Статья расходов", Type: "text", MaxLength: 500},
		{Key: "top_agreement_reference", Label: "Реквизиты договора", Type: "text", MaxLength: 1000},
		{Key: "payment_order_reference", Label: "Платёжное поручение", Type: "text", MaxLength: 1000},
		{Key: "spending_act_reference", Label: "Акт / отчёт о расходовании", Type: "text", MaxLength: 1000},
		{Key: "ano_letter_reference", Label: "Письмо-согласование АНО АЦ", Type: "text", MaxLength: 1000},
	}
}

// schoolProgramsCalc — дополнительные общеобразовательные программы для
// учащихся 5–11 классов. Сумма состоит из часов привлечённых сотрудников и
// полной стоимости разработки программ.
type schoolProgramsCalc struct{}

func schoolFundingFields() []FieldSpec {
	return []FieldSpec{
		{Key: "funding_source", Label: "Источник финансирования", Type: "text", Required: true, MaxLength: 500},
		{Key: "budget_funding", Label: "Бюджетное финансирование", Type: "select", Required: true, Options: []string{"absent", "full_or_partial"}},
		{Key: "citizen_funding", Label: "Финансирование средствами граждан", Type: "select", Required: true, Options: []string{"absent", "full_or_partial"}},
	}
}

func validateSchoolFunding(payload map[string]interface{}) error {
	budgetFunding, err := str(payload, "budget_funding")
	if err != nil {
		return err
	}
	if budgetFunding != "absent" {
		return fmt.Errorf("мероприятие не может быть зачтено: полностью или частично использованы средства бюджетов РФ")
	}
	citizenFunding, err := str(payload, "citizen_funding")
	if err != nil {
		return err
	}
	if citizenFunding != "absent" {
		return fmt.Errorf("мероприятие не может быть зачтено: полностью или частично использованы средства граждан")
	}
	return nil
}

func (schoolProgramsCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	if audience != models.AudienceSchool {
		return 0, fmt.Errorf("категория дополнительных школьных программ поддерживает только аудиторию school")
	}
	hours, err := nonNegativeNum(payload, "academic_hours")
	if err != nil {
		return 0, err
	}
	programs, err := nonNegativeNum(payload, "developed_programs_count")
	if err != nil {
		return 0, err
	}
	if hours == 0 && programs == 0 {
		return 0, fmt.Errorf("укажите академические часы или количество разработанных программ")
	}
	return round2(hours*schoolProgramAcademicHourRate + programs*schoolProgramDevelopmentRate), nil
}

func (schoolProgramsCalc) Fields() []FieldSpec {
	return append([]FieldSpec{
		{Key: "org_name", Label: "Наименование общеобразовательной организации", Type: "select", Required: true},
		{Key: "program_name", Label: "Наименование дополнительной общеобразовательной программы", Type: "text", Required: true},
		{Key: "employee_full_name", Label: "ФИО привлечённого сотрудника", Type: "text"},
		{Key: "academic_hours", Label: "Количество академических часов", Type: "number", Required: true},
		{Key: "developed_programs_count", Label: "Количество разработанных программ", Type: "number", Required: true, Integer: true},
		{Key: "students_count", Label: "Численность учащихся 5–11 классов", Type: "number", Required: true, Integer: true},
		{Key: "class_range", Label: "Классы", Type: "text"},
		{Key: "school_agreement_reference", Label: "Соглашение со школой / РОИВ", Type: "text", MaxLength: 1000},
		{Key: "participant_groups_reference", Label: "Реестр групп участников", Type: "text", MaxLength: 1000},
		{Key: "acceptance_act_reference", Label: "Акт приёмки", Type: "text", MaxLength: 1000},
	}, schoolFundingFields()...)
}

func (schoolProgramsCalc) Validate(payload map[string]interface{}) error {
	return validateSchoolFunding(payload)
}

// teacherTrainingCalc — программы повышения квалификации учителей
// информатики: разработка программы плюс обучение учителей по часовой ставке.
type teacherTrainingCalc struct{}

func (teacherTrainingCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	if audience != models.AudienceSchool {
		return 0, fmt.Errorf("категория повышения квалификации учителей поддерживает только аудиторию school")
	}
	programs, err := nonNegativeNum(payload, "developed_programs_count")
	if err != nil {
		return 0, err
	}
	hours, err := nonNegativeNum(payload, "academic_hours_per_teacher")
	if err != nil {
		return 0, err
	}
	teachers, err := nonNegativeNum(payload, "trained_teachers_count")
	if err != nil {
		return 0, err
	}
	if programs == 0 && (hours == 0 || teachers == 0) {
		return 0, fmt.Errorf("укажите разработанную программу либо часы и число обученных учителей")
	}
	return round2(programs*teacherTrainingDevelopmentRate + hours*teachers*teacherTrainingHourRate), nil
}

func (teacherTrainingCalc) Fields() []FieldSpec {
	return append([]FieldSpec{
		{Key: "org_name", Label: "Наименование образовательной организации", Type: "select", Required: true},
		{Key: "program_name", Label: "Наименование программы повышения квалификации", Type: "text", Required: true},
		{Key: "developed_programs_count", Label: "Количество разработанных программ", Type: "number", Required: true, Integer: true},
		{Key: "academic_hours_per_teacher", Label: "Количество академических часов на одного учителя", Type: "number", Required: true},
		{Key: "trained_teachers_count", Label: "Количество обученных учителей информатики", Type: "number", Required: true, Integer: true},
		{Key: "school_agreement_reference", Label: "Соглашение со школой / РОИВ", Type: "text", MaxLength: 1000},
		{Key: "participant_groups_reference", Label: "Реестр обученных учителей", Type: "text", MaxLength: 1000},
		{Key: "acceptance_act_reference", Label: "Акт приёмки", Type: "text", MaxLength: 1000},
	}, schoolFundingFields()...)
}

func (teacherTrainingCalc) Validate(payload map[string]interface{}) error {
	return validateSchoolFunding(payload)
}

// educationalContentCalc считает суммарные человеко-платформо-месяцы доступа
// отдельно для школьников и учителей.
type educationalContentCalc struct{}

func (educationalContentCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	if audience != models.AudienceSchool {
		return 0, fmt.Errorf("категория образовательного контента поддерживает только аудиторию school")
	}
	studentMonths, err := nonNegativeNum(payload, "student_platform_months")
	if err != nil {
		return 0, err
	}
	teacherMonths, err := nonNegativeNum(payload, "teacher_platform_months")
	if err != nil {
		return 0, err
	}
	if studentMonths == 0 && teacherMonths == 0 {
		return 0, fmt.Errorf("укажите доступ школьников или учителей")
	}
	return round2(studentMonths*studentPlatformMonthRate + teacherMonths*teacherPlatformMonthRate), nil
}

func (educationalContentCalc) Fields() []FieldSpec {
	return append([]FieldSpec{
		{Key: "org_name", Label: "Наименование общеобразовательной организации", Type: "select", Required: true},
		{Key: "platform_name", Label: "Наименование образовательной платформы", Type: "text", Required: true},
		{Key: "student_platform_months", Label: "Суммарные месяцы доступа всех учащихся", Type: "number", Required: true, Integer: true},
		{Key: "teacher_platform_months", Label: "Суммарные месяцы доступа всех учителей", Type: "number", Required: true, Integer: true},
		{Key: "digital_trace_reference", Label: "Описание/ссылка на подтверждение цифрового следа", Type: "text", Required: true},
		{Key: "school_agreement_reference", Label: "Соглашение со школой / РОИВ", Type: "text", MaxLength: 1000},
		{Key: "acceptance_act_reference", Label: "Акт приёмки доступа", Type: "text", MaxLength: 1000},
	}, schoolFundingFields()...)
}

func (educationalContentCalc) Validate(payload map[string]interface{}) error {
	return validateSchoolFunding(payload)
}
