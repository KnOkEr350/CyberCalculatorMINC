package calculators

import (
	"cybercalc/internal/models"
	"fmt"
)

// oodRpdCalc — раздел «ООП и РПД». Фиксированные суммы из таблицы ТЗ,
// зависят от вида документа (РПД/ООП), уровня образования (ВО/СПО)
// и вида активности (Разработка/Актуализация/Экспертиза).
type oodRpdCalc struct{}

// docType: "rpd" | "oop"; level: "vo" | "spo"; activityType: "development" | "update" | "expertise"
var oodRpdRates = map[string]map[string]map[string]float64{
	"rpd": {
		"vo":  {"development": 300000, "update": 160000, "expertise": 55000},
		"spo": {"development": 270750, "update": 150000, "expertise": 58060},
	},
	"oop": {
		"vo":  {"development": 2039850, "update": 626110, "expertise": 312300},
		"spo": {"development": 1731360, "update": 427440, "expertise": 171000},
	},
}

func (oodRpdCalc) Calculate(audience models.Audience, payload map[string]interface{}) (float64, error) {
	docType, err := str(payload, "doc_type")
	if err != nil {
		return 0, err
	}
	level, err := str(payload, "level")
	if err != nil {
		return 0, err
	}
	if (audience == models.AudienceVuz && level != "vo") || (audience == models.AudienceKolledj && level != "spo") {
		return 0, fmt.Errorf("уровень образования %q не соответствует аудитории %q", level, audience)
	}
	if audience != models.AudienceVuz && audience != models.AudienceKolledj {
		return 0, fmt.Errorf("категория «ООП и РПД» не поддерживает аудиторию %q", audience)
	}
	activityType, err := str(payload, "activity_type")
	if err != nil {
		return 0, err
	}

	byLevel, ok := oodRpdRates[docType]
	if !ok {
		return 0, fmt.Errorf("неизвестный вид документа: %s (ожидается rpd|oop)", docType)
	}
	byActivity, ok := byLevel[level]
	if !ok {
		return 0, fmt.Errorf("неизвестный уровень образования: %s (ожидается vo|spo)", level)
	}
	rate, ok := byActivity[activityType]
	if !ok {
		return 0, fmt.Errorf("неизвестный вид активности: %s (ожидается development|update|expertise)", activityType)
	}
	return round2(rate), nil
}

func (oodRpdCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select", Required: true},
		{Key: "doc_type", Label: "Вид документа", Type: "select", Required: true, Options: []string{"rpd", "oop"}},
		{Key: "level", Label: "Уровень образования", Type: "select", Required: true, Options: []string{"vo", "spo"}},
		{Key: "activity_type", Label: "Вид активности", Type: "select", Required: true, Options: []string{"development", "update", "expertise"}},
		{Key: "program_name", Label: "Наименование РПД/ООП", Type: "text", Required: true},
		{Key: "expert_full_name", Label: "ФИО эксперта", Type: "text"},
		{Key: "students_reach", Label: "Охват студентов", Type: "number", Integer: true},
	}
}
