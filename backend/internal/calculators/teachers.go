package calculators

import (
	"cybercalc/internal/models"
	teachingdomain "cybercalc/internal/modules/teaching/domain"
	"cybercalc/internal/tariffs"
	"fmt"
	"strings"
	"time"
)

// teachersCalc - раздел «Преподаватели-практики».
// Ставка из ТЗ: "Для вузов: Ак.ч. х 4 140 руб.; Для колледжей: Ак.ч. х 3900 руб."
type teachersCalc struct{}

// Ставки читаются из редакции поставки (DATA-07): одно значение в одном месте.
var (
	teacherRateVuz     = tariffs.Default().MustFloat(tariffs.TeacherHourVuz)
	teacherRateKolledj = tariffs.Default().MustFloat(tariffs.TeacherHourKolledj)
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
		{Key: "academic_period", Label: "Учебный период образовательной организации", Type: "text", MaxLength: 300, Widget: "academic_period"},
		{Key: "semester_start", Label: "Начало семестра", Type: "date"},
		{Key: "semester_end", Label: "Окончание семестра", Type: "date"},
		{Key: "semester_duration_weeks", Label: "Длительность семестра, недель", Type: "number", Integer: true, Minimum: 1, Maximum: 30},
		{Key: "training_direction", Label: "Направление подготовки (код и наименование)", Type: "text", MaxLength: 300, Widget: "specialty"},
		{Key: "institute", Label: "Институт / школа", Type: "text"},
		{Key: "faculty", Label: "Факультет", Type: "text"},
		{Key: "department", Label: "Кафедра", Type: "text"},
		{Key: "teaching_area", Label: "Название дисциплины по учебному плану", Type: "text", MaxLength: 500},
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
			"Безвозмездный договор ГПХ",
			"Трудовой договор с ИТ-компанией",
			"Договор пожертвования",
			"Прямой договор между ОО и ИТ-компанией",
		}},
		{Key: "specialty_code", Label: "Код ИТ-специальности по приказу № 27", Type: "text", Widget: "specialty"},
		{Key: "academic_group", Label: "Академическая группа", Type: "text"},
		{Key: "students_reach", Label: "Охват студентов", Type: "number", Integer: true},
		{Key: "academic_hours", Label: "Количество ак.ч.", Type: "number", Required: true},
		{Key: "class_schedule", Label: "Расписание занятий", Type: "text", MaxLength: 2000, Widget: "weekly_schedule"},
		{Key: "work_schedule", Label: "График работы", Type: "text", MaxLength: 2000, Widget: "weekly_schedule"},
		{Key: "employment_contract_reference", Label: "Реквизиты ТД / ГПХ", Type: "text", MaxLength: 1000},
		{Key: "donation_usage_report_reference", Label: "Реквизиты отчёта об использовании пожертвования", Type: "text", MaxLength: 1000},
		{Key: "appointment_order_reference", Label: "Реквизиты приказа о допуске к работе", Type: "text", MaxLength: 1000},
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
	start, hasStart := payload["semester_start"].(string)
	end, hasEnd := payload["semester_end"].(string)
	if hasStart && hasEnd && strings.TrimSpace(start) != "" && strings.TrimSpace(end) != "" {
		startDate, startErr := time.Parse("2006-01-02", start)
		endDate, endErr := time.Parse("2006-01-02", end)
		if startErr != nil || endErr != nil || endDate.Before(startDate) {
			return fmt.Errorf("окончание семестра не может быть раньше его начала")
		}
	}
	return nil
}
