package calculators

import (
	"cybercalc/internal/models"
	teachingdomain "cybercalc/internal/modules/teaching/domain"
	"fmt"
)

// teachersCalc — раздел «Преподаватели-практики».
// Ставка из ТЗ: "Для вузов: Ак.ч. х 4 140 руб.; Для колледжей: Ак.ч. х 3900 руб."
type teachersCalc struct{}

const (
	teacherRateVuz     = 4140.0
	teacherRateKolledj = 3900.0
)

func (teachersCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	hours, err := positiveNum(payload, "academic_hours")
	if err != nil {
		return 0, err
	}
	var rate float64
	switch audience {
	case models.AudienceVuz:
		rate = teacherRateVuz
	case models.AudienceKolledj:
		rate = teacherRateKolledj
	default:
		return 0, fmt.Errorf("категория «Преподаватели-практики» не поддерживает аудиторию %q", audience)
	}
	return round2(hours * rate), nil
}

func (teachersCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select", Required: true},
		// Optional at the shared calculator boundary until TCH-08 backfills
		// legacy teaching rows. The interactive UI requires this field for new
		// records, while existing imports and API clients remain compatible.
		{Key: "staff_member_id", Label: "Сотрудник ИТ-компании", Type: "select"},
		{Key: "course_name", Label: "Наименование курса", Type: "text", Required: true},
		{Key: "education_level", Label: "Уровень образовательной программы", Type: "select", Required: true, Options: []string{"bachelor", "master", "specialist", "spo"}},
		{Key: "semester", Label: "Семестр", Type: "number", Required: true, Integer: true, Minimum: 1, Maximum: 13},
		{Key: "training_direction", Label: "Направление подготовки", Type: "text"},
		{Key: "institute", Label: "Институт / школа", Type: "text"},
		{Key: "faculty", Label: "Факультет", Type: "text"},
		{Key: "department", Label: "Кафедра", Type: "text"},
		{Key: "teaching_area", Label: "Направление преподавания", Type: "text"},
		{Key: "teacher_full_name", Label: "ФИО преподавателя", Type: "text", Required: true},
		{Key: "employee_position", Label: "Должность в ИТ-компании", Type: "text"},
		{Key: "okz_code", Label: "Код ОКЗ", Type: "text"},
		{Key: "it_experience_days", Label: "Подтверждённый ИТ-стаж за последние 5 лет, дней", Type: "number", Integer: true, Minimum: 0, Maximum: 1827},
		{Key: "it_experience_reference", Label: "Основание подтверждения ИТ-стажа", Type: "text", MaxLength: 1000},
		// ТЗ §7.2: пять форм оформления привлечённого сотрудника. Прежние два
		// значения сохранены, чтобы не обесценить уже внесённые записи.
		{Key: "employment_form", Label: "Как оформлен", Type: "select", Required: true, Options: []string{
			"ТД по совместительству",
			"ГПХ",
			"Трудовой договор с ИТ-компанией",
			"Договор пожертвования",
			"Прямой договор между ОО и ИТ-компанией",
		}},
		{Key: "specialty_code", Label: "Код ИТ-специальности по приказу № 27", Type: "text"},
		{Key: "academic_group", Label: "Академическая группа", Type: "text"},
		{Key: "students_reach", Label: "Охват студентов", Type: "number", Integer: true},
		{Key: "academic_hours", Label: "Количество ак.ч.", Type: "number", Required: true},
		{Key: "class_schedule", Label: "Расписание занятий", Type: "text", MaxLength: 2000},
		{Key: "work_schedule", Label: "График работы", Type: "text", MaxLength: 2000},
		{Key: "employment_contract_reference", Label: "Реквизиты ТД / ГПХ", Type: "text", MaxLength: 1000},
		{Key: "donation_usage_report_reference", Label: "Реквизиты отчёта об использовании пожертвования", Type: "text", MaxLength: 1000},
		{Key: "appointment_order_reference", Label: "Реквизиты приказа о допуске", Type: "text", MaxLength: 1000},
		{Key: "individual_plan_reference", Label: "Реквизиты индивидуального плана", Type: "text", MaxLength: 1000},
	}
}

func (teachersCalc) Validate(payload map[string]interface{}) error {
	level, err := str(payload, "education_level")
	if err != nil {
		return err
	}
	semester, err := num(payload, "semester")
	if err != nil {
		return err
	}
	if err := teachingdomain.ValidateSemester(teachingdomain.EducationLevel(level), int(semester)); err != nil {
		return err
	}
	return nil
}
