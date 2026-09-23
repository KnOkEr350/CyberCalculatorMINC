// Package tariffs хранит ставки Методики Приказа Минцифры № 270 (DATA-07).
//
// Раньше одни и те же ставки были записаны в коде дважды — в точном пути
// расчёта и в приближённом — и менялись только правкой исходников. Теперь у
// каждой ставки есть код, а действующие значения хранятся в БД версиями с
// периодом действия и ссылкой на официальную редакцию. Значения, заложенные
// здесь, — это редакция, с которой система поставляется: миграция засевает её
// в БД, а тест сверяет обе стороны.
package tariffs

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"sort"
	"time"
)

// SeedSource — официальная редакция, с которой поставляется система.
const SeedSource = "Приказ Минцифры № 270, Методика (редакция поставки ТЗ 4.4)"

// Definition описывает одну ставку: что она оплачивает и в какой редакции
// поставляется. Amount — целые рубли: в Методике все ставки целые.
type Definition struct {
	Code     string
	Category string // вид активности, к которому относится ставка
	Label    string
	Unit     string
	Amount   int64
}

// Коды ставок. Практика трудоустройства считается по той же формуле, что и
// стажировка, и использует те же ставки.
const (
	TeacherHourVuz     = "teachers.academic_hour.vuz"
	TeacherHourKolledj = "teachers.academic_hour.kolledj"

	InternshipStudentHour = "internship.student_hour"
	InternshipMentorHour  = "internship.mentor_hour"

	SchoolProgramHour        = "it_clubs.academic_hour"
	SchoolProgramDevelopment = "it_clubs.program_development"

	TeacherTrainingHour        = "teacher_training.academic_hour"
	TeacherTrainingDevelopment = "teacher_training.program_development"

	PlatformStudentMonth = "edu_content.student_platform_month"
	PlatformTeacherMonth = "edu_content.teacher_platform_month"
)

// OODRPD возвращает код ставки «ООП и РПД»: вид документа × уровень × вид
// работы.
func OODRPD(docType, level, activity string) string {
	return "ood_rpd." + docType + "." + level + "." + activity
}

// definitions — редакция поставки. Порядок стабилен, чтобы выдача была
// воспроизводимой.
var definitions = []Definition{
	{TeacherHourVuz, "teachers", "Академический час преподавателя, ВО", "ак. час", 4140},
	{TeacherHourKolledj, "teachers", "Академический час преподавателя, СПО", "ак. час", 3900},

	{OODRPD("rpd", "vo", "development"), "ood_rpd", "РПД, ВО: разработка", "документ", 300000},
	{OODRPD("rpd", "vo", "update"), "ood_rpd", "РПД, ВО: актуализация", "документ", 160000},
	{OODRPD("rpd", "vo", "expertise"), "ood_rpd", "РПД, ВО: экспертиза", "документ", 55000},
	{OODRPD("rpd", "spo", "development"), "ood_rpd", "РПД, СПО: разработка", "документ", 270750},
	{OODRPD("rpd", "spo", "update"), "ood_rpd", "РПД, СПО: актуализация", "документ", 150000},
	{OODRPD("rpd", "spo", "expertise"), "ood_rpd", "РПД, СПО: экспертиза", "документ", 58060},
	{OODRPD("oop", "vo", "development"), "ood_rpd", "ООП, ВО: разработка", "документ", 2039850},
	{OODRPD("oop", "vo", "update"), "ood_rpd", "ООП, ВО: актуализация", "документ", 626110},
	{OODRPD("oop", "vo", "expertise"), "ood_rpd", "ООП, ВО: экспертиза", "документ", 312300},
	{OODRPD("oop", "spo", "development"), "ood_rpd", "ООП, СПО: разработка", "документ", 1731360},
	{OODRPD("oop", "spo", "update"), "ood_rpd", "ООП, СПО: актуализация", "документ", 427440},
	{OODRPD("oop", "spo", "expertise"), "ood_rpd", "ООП, СПО: экспертиза", "документ", 171000},

	{InternshipStudentHour, "internship", "Час нагрузки студента", "час", 800},
	{InternshipMentorHour, "internship", "Час нагрузки наставника", "час", 2390},

	{SchoolProgramHour, "it_clubs", "Академический час школьной программы", "ак. час", 4260},
	{SchoolProgramDevelopment, "it_clubs", "Разработка школьной программы", "программа", 530890},

	{TeacherTrainingHour, "teacher_training", "Академический час обучения учителя", "ак. час", 3790},
	{TeacherTrainingDevelopment, "teacher_training", "Разработка программы обучения учителей", "программа", 1408570},

	{PlatformStudentMonth, "edu_content", "Месяц доступа студента к платформе", "месяц", 6800},
	{PlatformTeacherMonth, "edu_content", "Месяц доступа учителя к платформе", "месяц", 8590},
}

// Definitions возвращает копию редакции поставки.
func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}

// CategoryCodes перечисляет коды ставок вида активности. Практика
// трудоустройства использует ставки стажировки.
func CategoryCodes(category string) []string {
	if category == "employment_practice" {
		category = "internship"
	}
	codes := []string{}
	for _, definition := range definitions {
		if definition.Category == category {
			codes = append(codes, definition.Code)
		}
	}
	return codes
}

// Rate — ставка, применённая к расчёту, вместе с версией, из которой она
// взята: по ней можно восстановить, по какой редакции посчитана сумма.
type Rate struct {
	Code      string
	Amount    *big.Rat
	VersionID int64 // 0 у ставок редакции поставки, не загруженных из БД
	Source    string
}

// Card — набор ставок, действующих для отчётного года.
type Card struct {
	rates map[string]Rate
}

// Default — карточка редакции поставки. Она нужна проверкам, которым не нужна
// БД, и как эталон для сверки с засеянными значениями.
func Default() Card {
	card := Card{rates: make(map[string]Rate, len(definitions))}
	for _, definition := range definitions {
		card.rates[definition.Code] = Rate{
			Code: definition.Code, Amount: big.NewRat(definition.Amount, 1), Source: SeedSource,
		}
	}
	return card
}

// Rat возвращает ставку точным числом. Незаданная ставка — ошибка расчёта, а
// не ноль: молча посчитать сумму без ставки хуже, чем отказать.
func (c Card) Rat(code string) (*big.Rat, error) {
	rate, ok := c.rates[code]
	if !ok {
		return nil, fmt.Errorf("тариф %q не задан", code)
	}
	return new(big.Rat).Set(rate.Amount), nil
}

// Float возвращает ставку для приближённых расчётов и проверок. Для незаданной
// ставки возвращает 0 и ошибку.
func (c Card) Float(code string) (float64, error) {
	amount, err := c.Rat(code)
	if err != nil {
		return 0, err
	}
	value, _ := amount.Float64()
	return value, nil
}

// MustFloat — для карточки поставки, где каждая ставка заведомо есть.
func (c Card) MustFloat(code string) float64 {
	value, err := c.Float(code)
	if err != nil {
		panic(err)
	}
	return value
}

// VersionIDs возвращает идентификаторы версий, по которым посчитан вид
// активности, — основание суммы для отчётности.
func (c Card) VersionIDs(category string) []int64 {
	ids := []int64{}
	for _, code := range CategoryCodes(category) {
		if rate, ok := c.rates[code]; ok && rate.VersionID > 0 {
			ids = append(ids, rate.VersionID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Rates возвращает ставки карточки в порядке поставки.
func (c Card) Rates() []Rate {
	out := make([]Rate, 0, len(c.rates))
	for _, definition := range definitions {
		if rate, ok := c.rates[definition.Code]; ok {
			out = append(out, rate)
		}
	}
	return out
}

// Querier — то, что нужно загрузке: и *sql.DB, и *sql.Tx.
type Querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// EffectiveOn — дата, на которую выбирается редакция для отчётного года.
// Редакция действует для отчётных годов, начинающихся не раньше её
// valid_from: сумма мероприятия не должна менять смысл задним числом внутри
// года, поэтому выбор идёт по началу года, а не по дате внесения.
func EffectiveOn(reportYear int) time.Time {
	return time.Date(reportYear, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// Load читает действующие на отчётный год версии. Если для какой-либо ставки
// версии нет, возвращается ошибка: неполная карточка дала бы неверную сумму.
func Load(ctx context.Context, db Querier, reportYear int) (Card, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT ON (rate_code) id,rate_code,amount_rub::text,source_reference
		FROM tariff_versions
		WHERE valid_from <= $1::date AND (valid_until IS NULL OR valid_until >= $1::date)
		ORDER BY rate_code,valid_from DESC`, EffectiveOn(reportYear))
	if err != nil {
		return Card{}, fmt.Errorf("прочитать тарифы: %w", err)
	}
	defer rows.Close()
	card := Card{rates: map[string]Rate{}}
	for rows.Next() {
		var rate Rate
		var amount string
		if err := rows.Scan(&rate.VersionID, &rate.Code, &amount, &rate.Source); err != nil {
			return Card{}, err
		}
		parsed, ok := new(big.Rat).SetString(amount)
		if !ok {
			return Card{}, fmt.Errorf("тариф %q: некорректная сумма %q", rate.Code, amount)
		}
		rate.Amount = parsed
		card.rates[rate.Code] = rate
	}
	if err := rows.Err(); err != nil {
		return Card{}, err
	}
	for _, definition := range definitions {
		if _, ok := card.rates[definition.Code]; !ok {
			return Card{}, fmt.Errorf("на %d год не задан тариф %q", reportYear, definition.Code)
		}
	}
	return card, nil
}
