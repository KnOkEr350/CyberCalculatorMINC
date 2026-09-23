package calculators

import (
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

// CalculateAmount is the persistence boundary. Input decimals and all
// intermediate arithmetic remain exact; only the final sum rounds to kopecks.
func CalculateAmount(code string, audience models.Audience, p map[string]interface{}) (money.Amount, error) {
	return calculateAmount(code, audience, p, nil)
}

// CalculateAmountWithRates uses monetary coefficients selected from the
// versioned DATA-07 registry. Formula structure stays in typed code.
func CalculateAmountWithRates(code string, audience models.Audience, p map[string]interface{}, rates map[string]string) (money.Amount, error) {
	if len(rates) == 0 && code != "top_it" && code != "minc_decision" {
		return 0, fmt.Errorf("для %s отсутствуют тарифы", code)
	}
	return calculateAmount(code, audience, p, rates)
}

func calculateAmount(code string, audience models.Audience, p map[string]interface{}, rates map[string]string) (money.Amount, error) {
	c, err := Get(code)
	if err != nil {
		return 0, err
	}
	var legacyResult float64
	if rates == nil {
		// Kept only for the legacy, non-persistence API and its conformance
		// tests. Production callers always provide a DATA-07 rate set.
		legacyResult, err = c.Calculate(audience, p)
		if err != nil {
			return 0, err
		}
	} else if err := validateRateDrivenFormula(code, audience, p); err != nil {
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
	mul := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
	add := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
	i := func(v int64) *big.Rat { return big.NewRat(v, 1) }
	rate := func(key string, legacy int64) *big.Rat {
		if rates == nil {
			return i(legacy)
		}
		value, ok := rates[key]
		if !ok {
			parseErr = fmt.Errorf("в версии тарифа отсутствует компонент %s", key)
			return new(big.Rat)
		}
		parsed, ok := new(big.Rat).SetString(value)
		if !ok || parsed.Sign() < 0 {
			parseErr = fmt.Errorf("некорректный тариф %s", key)
			return new(big.Rat)
		}
		return parsed
	}
	var result *big.Rat
	switch code {
	case "teachers":
		legacy := int64(4140)
		if audience == models.AudienceKolledj {
			legacy = 3900
		}
		result = mul(n("academic_hours"), rate("academic_hour", legacy))
	case "internship", "employment_practice":
		result = mul(add(mul(n("student_load_hours_per_month"), rate("student_hour", 800)), mul(n("mentor_load_hours_per_month"), rate("mentor_hour", 2390))), n("duration_months"))
	case "ood_rpd":
		docType, _ := p["doc_type"].(string)
		level, _ := p["level"].(string)
		activity, _ := p["activity_type"].(string)
		result = rate(docType+"."+level+"."+activity, int64(legacyResult))
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
		result = add(mul(n("academic_hours"), rate("academic_hour", 4260)), mul(n("developed_programs_count"), rate("developed_program", 530890)))
	case "teacher_training":
		result = add(mul(n("developed_programs_count"), rate("developed_program", 1408570)), mul(mul(n("academic_hours_per_teacher"), n("trained_teachers_count")), rate("academic_hour_per_teacher", 3790)))
	case "edu_content":
		result = add(mul(n("student_platform_months"), rate("student_platform_month", 6800)), mul(n("teacher_platform_months"), rate("teacher_platform_month", 8590)))
	default:
		return 0, fmt.Errorf("неизвестная формула")
	}
	if parseErr != nil {
		return 0, parseErr
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

// validateRateDrivenFormula checks formula shape without reading any monetary
// coefficient from the legacy calculators. This is the production path: all
// coefficients used below come exclusively from the selected DATA-07 version.
func validateRateDrivenFormula(code string, audience models.Audience, p map[string]interface{}) error {
	schoolOnly := func() error {
		if audience != models.AudienceSchool {
			return fmt.Errorf("категория %q поддерживает только аудиторию school", code)
		}
		return nil
	}
	switch code {
	case "teachers":
		if audience != models.AudienceVuz && audience != models.AudienceKolledj {
			return fmt.Errorf("категория «Преподаватели-практики» не поддерживает аудиторию %q", audience)
		}
		_, err := positiveNum(p, "academic_hours")
		return err
	case "internship", "employment_practice":
		student, err := nonNegativeNum(p, "student_load_hours_per_month")
		if err != nil {
			return err
		}
		mentor, err := nonNegativeNum(p, "mentor_load_hours_per_month")
		if err != nil {
			return err
		}
		if _, err := positiveNum(p, "duration_months"); err != nil {
			return err
		}
		if student == 0 && mentor == 0 {
			return fmt.Errorf("должна быть указана нагрузка студента или наставника")
		}
		return nil
	case "ood_rpd":
		docType, err := str(p, "doc_type")
		if err != nil {
			return err
		}
		level, err := str(p, "level")
		if err != nil {
			return err
		}
		activity, err := str(p, "activity_type")
		if err != nil {
			return err
		}
		if docType != "rpd" && docType != "oop" {
			return fmt.Errorf("неизвестный вид документа: %s (ожидается rpd|oop)", docType)
		}
		if activity != "development" && activity != "update" && activity != "expertise" {
			return fmt.Errorf("неизвестный вид активности: %s", activity)
		}
		if (audience == models.AudienceVuz && level != "vo") || (audience == models.AudienceKolledj && level != "spo") {
			return fmt.Errorf("уровень образования %q не соответствует аудитории %q", level, audience)
		}
		if audience != models.AudienceVuz && audience != models.AudienceKolledj {
			return fmt.Errorf("категория «ООП и РПД» не поддерживает аудиторию %q", audience)
		}
		return nil
	case "it_clubs":
		if err := schoolOnly(); err != nil {
			return err
		}
		hours, err := nonNegativeNum(p, "academic_hours")
		if err != nil {
			return err
		}
		programs, err := nonNegativeNum(p, "developed_programs_count")
		if err != nil {
			return err
		}
		if hours == 0 && programs == 0 {
			return fmt.Errorf("укажите академические часы или количество разработанных программ")
		}
		return nil
	case "teacher_training":
		if err := schoolOnly(); err != nil {
			return err
		}
		programs, err := nonNegativeNum(p, "developed_programs_count")
		if err != nil {
			return err
		}
		hours, err := nonNegativeNum(p, "academic_hours_per_teacher")
		if err != nil {
			return err
		}
		teachers, err := nonNegativeNum(p, "trained_teachers_count")
		if err != nil {
			return err
		}
		if programs == 0 && (hours == 0 || teachers == 0) {
			return fmt.Errorf("укажите разработанную программу либо часы и число обученных учителей")
		}
		return nil
	case "edu_content":
		if err := schoolOnly(); err != nil {
			return err
		}
		students, err := nonNegativeNum(p, "student_platform_months")
		if err != nil {
			return err
		}
		teachers, err := nonNegativeNum(p, "teacher_platform_months")
		if err != nil {
			return err
		}
		if students == 0 && teachers == 0 {
			return fmt.Errorf("укажите доступ школьников или учителей")
		}
		return nil
	default:
		return fmt.Errorf("неизвестная формула")
	}
}
