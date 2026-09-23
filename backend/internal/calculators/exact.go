package calculators

import (
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

// CalculateAmount считает сумму по ставкам редакции поставки. Рабочий путь
// сохранения использует CalculateAmountWith со ставками, действующими на
// отчётный год; этот вариант нужен проверкам без БД.
func CalculateAmount(code string, audience models.Audience, p map[string]interface{}) (money.Amount, error) {
	return CalculateAmountWith(tariffs.Default(), code, audience, p)
}

// CalculateAmountWith is the persistence boundary. Input decimals and all
// intermediate arithmetic remain exact; only the final sum rounds to kopecks.
// Ставки берутся из переданной карточки (DATA-07), а не из кода.
func CalculateAmountWith(card tariffs.Card, code string, audience models.Audience, p map[string]interface{}) (money.Amount, error) {
	c, err := Get(code)
	if err != nil {
		return 0, err
	}
	if _, err := c.Calculate(audience, p); err != nil {
		return 0, err
	}
	var parseErr error
	n := func(key string) *big.Rat {
		var s string
		switch v := p[key].(type) {
		case json.Number:
			s = v.String()
		case float64:
			s = strconv.FormatFloat(v, 'f', -1, 64)
		case int:
			s = strconv.Itoa(v)
		default:
			parseErr = fmt.Errorf("неверное число %s", key)
			return new(big.Rat)
		}
		r, ok := new(big.Rat).SetString(s)
		if !ok {
			parseErr = fmt.Errorf("неверное число %s", key)
			return new(big.Rat)
		}
		return r
	}
	var rateErr error
	rate := func(rateCode string) *big.Rat {
		value, err := card.Rat(rateCode)
		if err != nil {
			rateErr = err
			return new(big.Rat)
		}
		return value
	}
	text := func(key string) string {
		value, _ := p[key].(string)
		return value
	}
	mul := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
	add := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
	var result *big.Rat
	switch code {
	case "teachers":
		hourly := tariffs.TeacherHourVuz
		if audience == models.AudienceKolledj {
			hourly = tariffs.TeacherHourKolledj
		}
		result = mul(n("academic_hours"), rate(hourly))
	case "internship", "employment_practice":
		result = mul(add(mul(n("student_load_hours_per_month"), rate(tariffs.InternshipStudentHour)),
			mul(n("mentor_load_hours_per_month"), rate(tariffs.InternshipMentorHour))), n("duration_months"))
	case "ood_rpd":
		// Допустимость сочетания вида документа, уровня и работы проверена
		// выше через Calculate; здесь берётся ставка этого сочетания.
		result = rate(tariffs.OODRPD(text("doc_type"), text("level"), text("activity_type")))
	case "top_it":
		// TOP-02: в зачёт норматива идёт фактически списанная вузом сумма
		// (ТЗ §7.5). Раньше точный путь всегда брал объём по отчёту, поэтому
		// на экране показывалась списанная сумма, а в БД сохранялась
		// перечисленная — зачёт получался завышенным.
		amountKey := "cofinancing_amount_rub"
		if _, ok := p["actual_spent_amount_rub"]; ok {
			amountKey = "actual_spent_amount_rub"
		}
		result = n(amountKey)
	case "minc_decision":
		result = n("amount_manual")
	case "it_clubs":
		result = add(mul(n("academic_hours"), rate(tariffs.SchoolProgramHour)),
			mul(n("developed_programs_count"), rate(tariffs.SchoolProgramDevelopment)))
	case "teacher_training":
		result = add(mul(n("developed_programs_count"), rate(tariffs.TeacherTrainingDevelopment)),
			mul(mul(n("academic_hours_per_teacher"), n("trained_teachers_count")), rate(tariffs.TeacherTrainingHour)))
	case "edu_content":
		result = add(mul(n("student_platform_months"), rate(tariffs.PlatformStudentMonth)),
			mul(n("teacher_platform_months"), rate(tariffs.PlatformTeacherMonth)))
	default:
		return 0, fmt.Errorf("неизвестная формула")
	}
	if parseErr != nil {
		return 0, parseErr
	}
	if rateErr != nil {
		return 0, rateErr
	}
	amount, err := money.FromRat(result)
	if err != nil {
		return 0, err
	}
	if amount > 9_999_999_999_999_999 {
		return 0, fmt.Errorf("сумма превышает NUMERIC(16,2)")
	}
	return amount, nil
}
