package calculators

import (
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"testing"

	"cybercalc/internal/models"
	"cybercalc/internal/money"
)

// QA-01 / OOP-02 / TCH-04: соответствие формул Приложению № 6 к Приказу
// № 270. Главный объект проверки — CalculateAmount: именно его результат
// сохраняется в entries.formula_amount_rub при ручном вводе и XLSX-импорте,
// и тарифы в нём заданы отдельно от float-калькуляторов экранов.
//
// Тарифы ниже записаны строками ровно так, как они напечатаны в Приказе, —
// в тыс. руб. с десятичной запятой, — чтобы каждую цифру можно было сверить
// с первоисточником без пересчёта в уме.

// annex6 переводит тариф Приказа из «тыс. руб.» (например, "2039,85") в
// точную сумму в копейках.
func annex6(t *testing.T, thousandRub string) money.Amount {
	t.Helper()
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(thousandRub, ",", "."))
	if !ok {
		t.Fatalf("некорректный тариф в тесте: %q", thousandRub)
	}
	amount, err := money.FromRat(new(big.Rat).Mul(r, big.NewRat(1000, 1)))
	if err != nil {
		t.Fatal(err)
	}
	return amount
}

type conformanceCase struct {
	name     string
	category string
	audience models.Audience
	payload  map[string]interface{}
	// want — ожидаемая сумма, выраженная через тарифы Приказа.
	want func(t *testing.T) money.Amount
}

func times(t *testing.T, tariff string, units int64) money.Amount {
	return annex6(t, tariff) * money.Amount(units)
}

func conformanceCases() []conformanceCase {
	var cases []conformanceCase

	// Вид 1. Один академический час преподавания: ВО 4,14; СПО 3,9.
	cases = append(cases,
		conformanceCase{"Вид 1 ВО, 1 ак. час", "teachers", models.AudienceVuz,
			map[string]interface{}{"academic_hours": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "4,14") }},
		conformanceCase{"Вид 1 СПО, 1 ак. час", "teachers", models.AudienceKolledj,
			map[string]interface{}{"academic_hours": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "3,9") }},
		conformanceCase{"Вид 1 ВО, 64 ак. часа (пример ТЗ: 264 960 ₽)", "teachers", models.AudienceVuz,
			map[string]interface{}{"academic_hours": 64.0},
			func(t *testing.T) money.Amount { return times(t, "4,14", 64) }},
		conformanceCase{"Вид 1 ВО, полчаса — без потери копеек", "teachers", models.AudienceVuz,
			map[string]interface{}{"academic_hours": 0.5},
			func(t *testing.T) money.Amount { return annex6(t, "4,14") / 2 }},
	)

	// Вид 2. Астрономический час стажёра 0,8; час наставника 2,39 (ВО и СПО
	// одинаково). Стажировка и практика с трудоустройством — одна формула.
	for _, category := range []string{"internship", "employment_practice"} {
		cases = append(cases,
			conformanceCase{category + ": 1 ч студента", category, models.AudienceVuz,
				map[string]interface{}{"student_load_hours_per_month": 1.0, "mentor_load_hours_per_month": 0.0, "duration_months": 1.0},
				func(t *testing.T) money.Amount { return annex6(t, "0,8") }},
			conformanceCase{category + ": 1 ч наставника", category, models.AudienceKolledj,
				map[string]interface{}{"student_load_hours_per_month": 0.0, "mentor_load_hours_per_month": 1.0, "duration_months": 1.0},
				func(t *testing.T) money.Amount { return annex6(t, "2,39") }},
			conformanceCase{category + ": пример ТЗ 80/20 ч × 3 мес = 335 400 ₽", category, models.AudienceVuz,
				map[string]interface{}{"student_load_hours_per_month": 80.0, "mentor_load_hours_per_month": 20.0, "duration_months": 3.0},
				func(t *testing.T) money.Amount { return times(t, "0,8", 240) + times(t, "2,39", 60) }},
		)
	}

	// Вид 3. Все 12 комбинаций ООП/РПД × ВО/СПО × разработка/актуализация/экспертиза.
	oop := []struct {
		doc, level, activity, tariff string
		audience                     models.Audience
	}{
		{"oop", "vo", "development", "2039,85", models.AudienceVuz},
		{"oop", "vo", "update", "626,11", models.AudienceVuz},
		{"oop", "vo", "expertise", "312,3", models.AudienceVuz},
		{"oop", "spo", "development", "1731,36", models.AudienceKolledj},
		{"oop", "spo", "update", "427,44", models.AudienceKolledj},
		{"oop", "spo", "expertise", "171", models.AudienceKolledj},
		{"rpd", "vo", "development", "300,00", models.AudienceVuz},
		{"rpd", "vo", "update", "160", models.AudienceVuz},
		{"rpd", "vo", "expertise", "55,00", models.AudienceVuz},
		{"rpd", "spo", "development", "270,75", models.AudienceKolledj},
		{"rpd", "spo", "update", "150", models.AudienceKolledj},
		// Экспертиза РПД для СПО дороже, чем для ВО, — так в самом Приказе.
		{"rpd", "spo", "expertise", "58,06", models.AudienceKolledj},
	}
	for _, c := range oop {
		c := c
		cases = append(cases, conformanceCase{
			"Вид 3 " + c.doc + " " + c.level + " " + c.activity, "ood_rpd", c.audience,
			map[string]interface{}{"doc_type": c.doc, "level": c.level, "activity_type": c.activity},
			func(t *testing.T) money.Amount { return annex6(t, c.tariff) },
		})
	}

	// Вид 6. Час работы 4,26; разработка программы 530,89.
	cases = append(cases,
		conformanceCase{"Вид 6: 1 ак. час", "it_clubs", models.AudienceSchool,
			map[string]interface{}{"academic_hours": 1.0, "developed_programs_count": 0.0},
			func(t *testing.T) money.Amount { return annex6(t, "4,26") }},
		conformanceCase{"Вид 6: 1 программа", "it_clubs", models.AudienceSchool,
			map[string]interface{}{"academic_hours": 0.0, "developed_programs_count": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "530,89") }},
		conformanceCase{"Вид 6: пример ТЗ 1 программа + 72 ч = 837 610 ₽", "it_clubs", models.AudienceSchool,
			map[string]interface{}{"academic_hours": 72.0, "developed_programs_count": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "530,89") + times(t, "4,26", 72) }},
	)

	// Вид 7. Разработка 1408,57; час на одного учителя 3,79 (часы × учителя).
	cases = append(cases,
		conformanceCase{"Вид 7: 1 программа", "teacher_training", models.AudienceSchool,
			map[string]interface{}{"developed_programs_count": 1.0, "academic_hours_per_teacher": 0.0, "trained_teachers_count": 0.0},
			func(t *testing.T) money.Amount { return annex6(t, "1408,57") }},
		conformanceCase{"Вид 7: 1 ч на 1 учителя", "teacher_training", models.AudienceSchool,
			map[string]interface{}{"developed_programs_count": 0.0, "academic_hours_per_teacher": 1.0, "trained_teachers_count": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "3,79") }},
		conformanceCase{"Вид 7: пример ADR-12 — 1 программа, 72 ч, 10 учителей", "teacher_training", models.AudienceSchool,
			map[string]interface{}{"developed_programs_count": 1.0, "academic_hours_per_teacher": 72.0, "trained_teachers_count": 10.0},
			func(t *testing.T) money.Amount { return annex6(t, "1408,57") + times(t, "3,79", 720) }},
	)

	// Вид 8. Месяц доступа школьника 6,8; учителя 8,59.
	cases = append(cases,
		conformanceCase{"Вид 8: 1 мес. школьника", "edu_content", models.AudienceSchool,
			map[string]interface{}{"student_platform_months": 1.0, "teacher_platform_months": 0.0},
			func(t *testing.T) money.Amount { return annex6(t, "6,8") }},
		conformanceCase{"Вид 8: 1 мес. учителя", "edu_content", models.AudienceSchool,
			map[string]interface{}{"student_platform_months": 0.0, "teacher_platform_months": 1.0},
			func(t *testing.T) money.Amount { return annex6(t, "8,59") }},
	)
	return cases
}

func TestCalculateAmountMatchesOrder270Annex6(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CalculateAmount(tc.category, tc.audience, tc.payload)
			if err != nil {
				t.Fatalf("CalculateAmount(): %v", err)
			}
			if want := tc.want(t); got != want {
				t.Fatalf("CalculateAmount() = %s ₽, по Приложению № 6 ожидается %s ₽", got, want)
			}
		})
	}
}

// Сохраняемая сумма (CalculateAmount) и сумма, которую пользователь видит на
// экране (Calculate), не должны расходиться: тарифы продублированы в двух
// местах, и правка только одного из них должна ронять этот тест.
func TestCalculateAmountAgreesWithScreenCalculator(t *testing.T) {
	for _, tc := range conformanceCases() {
		t.Run(tc.name, func(t *testing.T) {
			calc, err := Get(tc.category)
			if err != nil {
				t.Fatal(err)
			}
			screen, err := calc.Calculate(tc.audience, tc.payload)
			if err != nil {
				t.Fatalf("Calculate(): %v", err)
			}
			stored, err := CalculateAmount(tc.category, tc.audience, tc.payload)
			if err != nil {
				t.Fatalf("CalculateAmount(): %v", err)
			}
			if math.Abs(screen-stored.Rubles()) > 0.005 {
				t.Fatalf("экран показывает %.2f ₽, а в БД сохранится %s ₽", screen, stored)
			}
		})
	}
}

// Числа из HTTP-запросов приходят как json.Number: точный путь не должен
// терять копейки там, где float64 даёт хвост двоичной дроби.
func TestCalculateAmountIsExactForDecimalInput(t *testing.T) {
	got, err := CalculateAmount("teachers", models.AudienceVuz, map[string]interface{}{"academic_hours": json.Number("0.1")})
	if err != nil {
		t.Fatal(err)
	}
	if want := money.Amount(41400); got != want { // 0,1 ч × 4 140 ₽ = 414,00 ₽ ровно
		t.Fatalf("CalculateAmount(0.1 ч) = %s ₽, want %s ₽", got, want)
	}
}

func TestCalculateAmountRejectsWrongAudience(t *testing.T) {
	cases := []struct {
		category string
		audience models.Audience
		payload  map[string]interface{}
	}{
		// ВО-уровень ООП/РПД нельзя провести по тарифу колледжа и наоборот.
		{"ood_rpd", models.AudienceKolledj, map[string]interface{}{"doc_type": "oop", "level": "vo", "activity_type": "development"}},
		{"ood_rpd", models.AudienceVuz, map[string]interface{}{"doc_type": "rpd", "level": "spo", "activity_type": "expertise"}},
		// Школьные виды 6–8 не применяются к вузам.
		{"it_clubs", models.AudienceVuz, map[string]interface{}{"academic_hours": 1.0, "developed_programs_count": 0.0}},
	}
	for _, tc := range cases {
		if amount, err := CalculateAmount(tc.category, tc.audience, tc.payload); err == nil {
			t.Fatalf("%s/%s: ожидалась ошибка аудитории, получена сумма %s", tc.category, tc.audience, amount)
		}
	}
}
