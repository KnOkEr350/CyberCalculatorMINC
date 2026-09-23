// Package exchange реализует обмен данными между экземплярами системы:
// формат защищённого пакета .pkg, экспорт с подписью и шифрованием, импорт с
// проверкой до разбора содержимого, сопоставление записей, сравнение и протокол
// разногласий. Пакет не обращается к БД: применение результата к данным —
// отдельный шаг под контролем оператора.
package exchange

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

var (
	ErrNoMatchKey   = errors.New("запись невозможно сопоставить: нет ни ФИО, ни предмета")
	ErrDuplicateKey = errors.New("несколько записей имеют один составной ключ")
)

// Record — запись мероприятия в обмене. Идентификаторы экземпляра (UUID
// партнёра, сотрудника, наставника) в запись не входят по смыслу: у другого
// экземпляра они другие, и сопоставлять по ним нельзя.
type Record struct {
	CategoryCode string                 `json:"category_code"`
	Audience     string                 `json:"audience"`
	AmountRub    string                 `json:"amount_rub"`
	Payload      map[string]interface{} `json:"payload"`
}

// Key — составной ключ сопоставления: вид + специальность + ФИО +
// дисциплина/предмет + период. Правила нормализации описаны в
// docs/PACKAGE_EXCHANGE.md и закреплены тестами.
type Key struct {
	Category  string
	Specialty string
	Person    string
	Subject   string
	Period    string
}

func (k Key) String() string {
	return strings.Join([]string{k.Category, k.Specialty, k.Person, k.Subject, k.Period}, " | ")
}

// keyFields — какие поля записи какого вида образуют части ключа. Список
// ищется по порядку: берётся первое непустое значение, поэтому старые записи с
// прежними названиями полей сопоставляются так же, как новые.
type keyFields struct {
	person, specialty, subject, period []string
	// periodPair — поля начала и конца, если период задаётся датами.
	periodPair [2]string
}

var keyFieldsByCategory = map[string]keyFields{
	"teachers": {
		person: []string{"teacher_full_name"}, specialty: []string{"specialty_code", "training_direction"},
		subject: []string{"course_name"}, period: []string{"semester"},
	},
	"internship":          internshipKey(),
	"employment_practice": internshipKey(),
	"ood_rpd": {
		person: []string{"expert_full_name"}, specialty: []string{"specialty_code"},
		subject: []string{"program_name"},
	},
	"top_it": {subject: []string{"project_name", "program_name"}, period: []string{"program_wave"}},
	"it_clubs": {
		person: []string{"employee_full_name"}, subject: []string{"program_name"},
	},
	"teacher_training": {subject: []string{"program_name"}},
	"edu_content": {
		subject:    []string{"platform_name"},
		periodPair: [2]string{"digital_trace_period_start", "digital_trace_period_end"},
	},
	"minc_decision": {
		subject: []string{"decision_number", "decision_reference", "instruction_reference"},
		period:  []string{"implementation_start"},
	},
}

func internshipKey() keyFields {
	return keyFields{
		person: []string{"student_full_name"}, specialty: []string{"specialty_code"},
		subject: []string{"course"}, period: []string{"period"},
		periodPair: [2]string{"period_start", "period_end"},
	}
}

// Subject-дополнения: для ООП/РПД предметом служит и вид документа с видом
// работы — «разработка РПД» и «экспертиза РПД» одной программы разные записи.
var subjectQualifiers = map[string][]string{
	"ood_rpd": {"doc_type", "activity_type"},
}

// KeyOf строит составной ключ записи. Запись, у которой нет ни ФИО, ни
// предмета, сопоставить нельзя: пустой ключ совпал бы со всеми такими же.
func KeyOf(record Record) (Key, error) {
	fields, ok := keyFieldsByCategory[record.CategoryCode]
	if !ok {
		return Key{}, fmt.Errorf("вид %q не участвует в обмене", record.CategoryCode)
	}
	subject := foldText(firstText(record.Payload, fields.subject))
	for _, qualifier := range subjectQualifiers[record.CategoryCode] {
		subject += " " + foldText(text(record.Payload, qualifier))
	}
	key := Key{
		Category:  record.CategoryCode,
		Specialty: foldCode(firstText(record.Payload, fields.specialty)),
		Person:    foldText(firstText(record.Payload, fields.person)),
		Subject:   strings.TrimSpace(subject),
		Period:    period(record.Payload, fields),
	}
	if key.Person == "" && key.Subject == "" {
		return Key{}, ErrNoMatchKey
	}
	return key, nil
}

func period(payload map[string]interface{}, fields keyFields) string {
	if fields.periodPair[0] != "" {
		start, end := foldCode(text(payload, fields.periodPair[0])), foldCode(text(payload, fields.periodPair[1]))
		if start != "" || end != "" {
			return start + ".." + end
		}
	}
	if value := foldCode(firstText(payload, fields.period)); value != "" {
		return value
	}
	return ""
}

func firstText(payload map[string]interface{}, keys []string) string {
	for _, key := range keys {
		if value := text(payload, key); value != "" {
			return value
		}
	}
	return ""
}

// text приводит значение поля к строке. Числа из JSON и из БД записываются
// одинаково: «3», «3.0» и 3 — один и тот же семестр.
func text(payload map[string]interface{}, key string) string {
	switch value := payload[key].(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(value)
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return numberText(new(big.Rat).SetFloat64(value))
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case interface{ String() string }: // json.Number
		if rat, ok := new(big.Rat).SetString(value.String()); ok {
			return numberText(rat)
		}
		return strings.TrimSpace(value.String())
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func numberText(rat *big.Rat) string {
	if rat == nil {
		return ""
	}
	if rat.IsInt() {
		return rat.Num().String()
	}
	return rat.FloatString(6)
}

// foldText — нормализация ФИО и названий. Правила: регистр не важен; «ё» и
// «е» равны; любая пунктуация и любое число пробелов — один пробел. Инициалы
// не раскрываются и не сокращаются: «Иванов И. И.» и «Иванов Иван Иванович»
// — разные ключи, потому что нечёткое сопоставление людей даёт ложные
// совпадения, а ложное совпадение опаснее пропущенного.
func foldText(value string) string {
	var out strings.Builder
	pendingSpace := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingSpace && out.Len() > 0 {
				out.WriteByte(' ')
			}
			pendingSpace = false
			r = unicode.ToLower(r)
			if r == 'ё' {
				r = 'е'
			}
			out.WriteRune(r)
		default:
			pendingSpace = true
		}
	}
	return out.String()
}

// foldCode — нормализация кодов и дат: пробелы убираются, регистр не важен,
// точки и дефисы сохраняются («09.03.01», «2026-09-01»).
func foldCode(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-':
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

// IndexByKey раскладывает записи по ключам. Повтор ключа внутри одной стороны
// — не «последняя запись побеждает», а явная ошибка: какую из записей
// сравнивать, решает человек.
func IndexByKey(records []Record) (map[string]Record, []Key, error) {
	index := make(map[string]Record, len(records))
	duplicates := []Key{}
	for _, record := range records {
		key, err := KeyOf(record)
		if err != nil {
			return nil, nil, err
		}
		id := key.String()
		if _, exists := index[id]; exists {
			duplicates = append(duplicates, key)
			continue
		}
		index[id] = record
	}
	if len(duplicates) > 0 {
		return index, duplicates, ErrDuplicateKey
	}
	return index, nil, nil
}
