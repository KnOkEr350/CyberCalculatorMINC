package calculators

import (
	"cybercalc/internal/models"
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
		{Key: "course_name", Label: "Наименование курса", Type: "text", Required: true},
		{Key: "department", Label: "Кафедра", Type: "text"},
		{Key: "teaching_area", Label: "Направление преподавания", Type: "text"},
		{Key: "teacher_full_name", Label: "ФИО преподавателя", Type: "text", Required: true},
		{Key: "employment_form", Label: "Как оформлен", Type: "select", Required: true, Options: []string{"ТД по совместительству", "ГПХ"}},
		{Key: "students_reach", Label: "Охват студентов", Type: "number", Integer: true},
		{Key: "academic_hours", Label: "Количество ак.ч.", Type: "number", Required: true},
	}
}
