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
	mul := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
	add := func(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
	i := func(v int64) *big.Rat { return big.NewRat(v, 1) }
	var result *big.Rat
	switch code {
	case "teachers":
		rate := int64(4140)
		if audience == models.AudienceKolledj {
			rate = 3900
		}
		result = mul(n("academic_hours"), i(rate))
	case "internship", "employment_practice":
		result = mul(add(mul(n("student_load_hours_per_month"), i(800)), mul(n("mentor_load_hours_per_month"), i(2390))), n("duration_months"))
	case "ood_rpd":
		rate, _ := c.Calculate(audience, p)
		result = i(int64(rate))
	case "top_it":
		result = n("cofinancing_amount_rub")
	case "minc_decision":
		result = n("amount_manual")
	case "it_clubs":
		result = add(mul(n("academic_hours"), i(4260)), mul(n("developed_programs_count"), i(530890)))
	case "teacher_training":
		result = add(mul(n("developed_programs_count"), i(1408570)), mul(mul(n("academic_hours_per_teacher"), n("trained_teachers_count")), i(3790)))
	case "edu_content":
		result = add(mul(n("student_platform_months"), i(6800)), mul(n("teacher_platform_months"), i(8590)))
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
