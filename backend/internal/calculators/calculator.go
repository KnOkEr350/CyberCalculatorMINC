// Package calculators реализует формулы оценки затрат из Методики,
// приложенной к Приказу Минцифры. Все ставки хранятся в рублях, хотя в
// Методике они приведены в тысячах рублей.
package calculators

import (
	"cybercalc/internal/models"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Calculator interface {
	// Calculate возвращает рассчитанную сумму в рублях по данным payload.
	Calculate(audience models.Audience, payload map[string]interface{}) (float64, error)
	// Fields описывает ожидаемые поля payload — используется UI и для валидации.
	Fields() []FieldSpec
}

type FieldSpec struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Type      string   `json:"type"` // number|text|select
	Required  bool     `json:"required"`
	Options   []string `json:"options,omitempty"`
	Integer   bool     `json:"integer,omitempty"`
	MaxLength int      `json:"max_length,omitempty"`
}

const (
	defaultTextMaxLength = 1000
	maxPayloadNumber     = 1_000_000_000_000.0
	maxCalculatedAmount  = 99_999_999_999_999.99 // NUMERIC(16,2)
)

var registry = map[string]Calculator{
	"teachers":            teachersCalc{},
	"ood_rpd":             oodRpdCalc{},
	"internship":          internshipCalc{},
	"employment_practice": internshipCalc{}, // формула идентична "Стажировкам" (см. ТЗ, раздел "Практика с трудоустройством")
	"top_it":              topITCalc{},
	"minc_decision":       manualCalc{},
	"it_clubs":            schoolProgramsCalc{},
	"teacher_training":    teacherTrainingCalc{},
	"edu_content":         educationalContentCalc{},
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

func nonNegativeNum(payload map[string]interface{}, key string) (float64, error) {
	v, err := num(payload, key)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, fmt.Errorf("поле %q должно быть неотрицательным конечным числом", key)
	}
	return v, nil
}

func positiveNum(payload map[string]interface{}, key string) (float64, error) {
	v, err := nonNegativeNum(payload, key)
	if err != nil {
		return 0, err
	}
	if v == 0 {
		return 0, fmt.Errorf("поле %q должно быть больше нуля", key)
	}
	return v, nil
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
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("поле %q не должно быть пустым", key)
	}
	return s, nil
}

// ValidatePayload проверяет обязательные поля динамической формы до запуска
// формулы. Это не даёт обойти UI и сохранить отрицательные числа или пустые
// обязательные реквизиты прямым HTTP-запросом.
func ValidatePayload(c Calculator, payload map[string]interface{}) error {
	if payload == nil {
		return fmt.Errorf("payload обязателен")
	}
	fields := c.Fields()
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field.Key] = struct{}{}
	}
	for key := range payload {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("неизвестное поле %q", key)
		}
	}
	for _, field := range fields {
		value, exists := payload[field.Key]
		if !exists || value == nil {
			if field.Required {
				return fmt.Errorf("поле %q обязательно", field.Key)
			}
			continue
		}

		switch field.Type {
		case "number":
			number, err := nonNegativeNum(payload, field.Key)
			if err != nil {
				return err
			}
			if number > maxPayloadNumber {
				return fmt.Errorf("поле %q превышает допустимое значение", field.Label)
			}
			if field.Integer && math.Trunc(number) != number {
				return fmt.Errorf("поле %q должно быть целым числом", field.Label)
			}
		case "text", "select":
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("поле %q должно быть строкой", field.Key)
			}
			s = strings.TrimSpace(s)
			if field.Required && s == "" {
				return fmt.Errorf("поле %q не должно быть пустым", field.Key)
			}
			maxLength := field.MaxLength
			if maxLength == 0 {
				maxLength = defaultTextMaxLength
			}
			if utf8.RuneCountInString(s) > maxLength {
				return fmt.Errorf("поле %q не должно превышать %d символов", field.Label, maxLength)
			}
			for _, r := range s {
				if unicode.IsControl(r) {
					return fmt.Errorf("поле %q содержит недопустимые управляющие символы", field.Label)
				}
			}
			payload[field.Key] = s
			if field.Type == "select" && len(field.Options) > 0 && s != "" {
				valid := false
				for _, option := range field.Options {
					if s == option {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("поле %q содержит недопустимое значение", field.Key)
				}
			}
		default:
			return fmt.Errorf("поле %q имеет неизвестный тип %q", field.Key, field.Type)
		}
	}
	return nil
}

// ValidateAmount гарантирует, что результат формулы можно безопасно сохранить
// в колонку NUMERIC(16,2), даже если API вызывается без интерфейса.
func ValidateAmount(amount float64) error {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return fmt.Errorf("рассчитанная сумма должна быть неотрицательным конечным числом")
	}
	if amount > maxCalculatedAmount {
		return fmt.Errorf("рассчитанная сумма превышает допустимый предел")
	}
	return nil
}
