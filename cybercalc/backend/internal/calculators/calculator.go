// Package calculators реализует формулы расчёта стоимости активностей.
//
// Источники сумм:
//   - Ставки по категориям "Преподаватели-практики", "ООП и РПД",
//     "Стажировки"/"Практика с трудоустройством" взяты дословно из таблиц
//     технического задания (rev-SVI-0109_ТЗ_Калькулятор...).
//   - Сам Приказ Минцифры (Приказ_270_2026) задаёт правило "не менее 3% от
//     сэкономленных средств" (см. Приложение 5, Таблица 1) — это учтено на
//     уровне дашборда (models/handlers/dashboard.go), а не на уровне формул
//     конкретных активностей: приказ регулирует механизм соглашений, а не
//     прейскурант, — ставки на единицы активности в самом приказе не заданы.
//   - Категории "Реализация по Решению Минцифры", "ИТ-кружки для
//     школьников" и "Образовательный контент" упомянуты в ТЗ только как
//     строки сводной таблицы, без формулы и без ставок. Для них сделан
//     осознанный выбор: сумма вводится пользователем напрямую (поле
//     amount_manual), см. DECISIONS.md — при появлении официальной ставки
//     достаточно добавить новый calculator и не менять API.
package calculators

import (
	"cybercalc/internal/models"
	"fmt"
)

type Calculator interface {
	// Calculate возвращает рассчитанную сумму в рублях по данным payload.
	Calculate(audience models.Audience, payload map[string]interface{}) (float64, error)
	// Fields описывает ожидаемые поля payload — используется UI и для валидации.
	Fields() []FieldSpec
}

type FieldSpec struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // number|text|select
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

var registry = map[string]Calculator{
	"teachers":            teachersCalc{},
	"ood_rpd":             oodRpdCalc{},
	"internship":          internshipCalc{},
	"employment_practice": internshipCalc{}, // формула идентична "Стажировкам" (см. ТЗ, раздел "Практика с трудоустройством")
	"top_it":              internshipCalc{}, // ТЗ приводит ту же формулу расчёта и для раздела «ТОП ИТ»
	"minc_decision":       manualCalc{},
	"it_clubs":            manualCalc{},
	"edu_content":         manualCalc{},
}

func Get(categoryCode string) (Calculator, error) {
	c, ok := registry[categoryCode]
	if !ok {
		return nil, fmt.Errorf("неизвестная категория активности: %s", categoryCode)
	}
	return c, nil
}

func num(payload map[string]interface{}, key string) (float64, error) {
	v, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("поле %q обязательно", key)
	}
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("поле %q должно быть числом", key)
	}
}

func str(payload map[string]interface{}, key string) (string, error) {
	v, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("поле %q обязательно", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("поле %q должно быть строкой", key)
	}
	return s, nil
}
