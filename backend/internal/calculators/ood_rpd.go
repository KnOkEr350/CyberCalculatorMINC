package calculators

import (
	"cybercalc/internal/models"
	"cybercalc/internal/tariffs"
	"fmt"
)

// oodRpdCalc — раздел «ООП и РПД». Фиксированные суммы из таблицы ТЗ,
// зависят от вида документа (РПД/ООП), уровня образования (ВО/СПО)
// и вида активности (Разработка/Актуализация/Экспертиза).
type oodRpdCalc struct{}

// docType: "rpd" | "oop"; level: "vo" | "spo"; activityType: "development" | "update" | "expertise".
// Суммы читаются из редакции поставки (DATA-07): таблица ставок живёт в одном
// месте, а здесь остаётся только допустимость сочетаний.
var (
	oodRpdDocTypes = []string{"rpd", "oop"}
	oodRpdLevels   = []string{"vo", "spo"}
)

func oodRpdRate(docType, level, activity string) (float64, bool) {
	value, err := tariffs.Default().Float(tariffs.OODRPD(docType, level, activity))
	return value, err == nil
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

	if !contains(oodRpdDocTypes, docType) {
		return 0, fmt.Errorf("неизвестный вид документа: %s (ожидается rpd|oop)", docType)
	}
	if !contains(oodRpdLevels, level) {
		return 0, fmt.Errorf("неизвестный уровень образования: %s (ожидается vo|spo)", level)
	}
	rate, ok := oodRpdRate(docType, level, activityType)
	if !ok {
		return 0, fmt.Errorf("неизвестный вид активности: %s (ожидается development|update|expertise)", activityType)
	}
	return round2(rate), nil
}

func (oodRpdCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select", Required: true},
		{Key: "doc_type", Label: "Вид документа", Type: "select", Required: true, Options: []string{"rpd", "oop"}},
		{Key: "semester", Label: "Семестр", Type: "number", Integer: true, Minimum: 1, Maximum: 13},
		{Key: "level", Label: "Уровень образования", Type: "select", Required: true, Options: []string{"vo", "spo"}},
		{Key: "activity_type", Label: "Вид активности", Type: "select", Required: true, Options: []string{"development", "update", "expertise"}},
		{Key: "program_name", Label: "Наименование РПД/ООП", Type: "text", Required: true},
		{Key: "expert_full_name", Label: "ФИО эксперта", Type: "text"},
		{Key: "students_reach", Label: "Охват студентов", Type: "number", Integer: true},
		{Key: "specialty_code", Label: "Код ИТ-специальности по приказу № 27", Type: "text"},
		{Key: "project_document_reference", Label: "Реквизиты проекта ООП/РПД", Type: "text", MaxLength: 1000},
		{Key: "expert_conclusion_reference", Label: "Реквизиты экспертного заключения", Type: "text", MaxLength: 1000},
		{Key: "approval_reference", Label: "Решение об утверждении / учёный совет", Type: "text", MaxLength: 1000},
	}
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
