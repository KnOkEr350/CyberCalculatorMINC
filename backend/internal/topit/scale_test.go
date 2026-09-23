package topit

import (
	"testing"

	"cybercalc/internal/money"
)

// ADR-14: границы шкалы 70% и 100% и отсутствие плана.
func TestScaleBoundaries(t *testing.T) {
	rub := func(v int64) money.Amount { return money.Amount(v * 100) }
	cases := []struct {
		name           string
		planned, spent money.Amount
		state          string
		percent        float64
	}{
		{"план не задан", 0, rub(500), "unknown", 0},
		{"ровно план", rub(5000000), rub(5000000), "green", 100},
		{"сверх плана", rub(5000000), rub(6000000), "green", 120},
		{"без одной копейки до плана", rub(1000), rub(1000) - 1, "yellow", 99.9},
		{"ровно 70%", rub(1000), rub(700), "yellow", 70},
		{"на копейку ниже 70%", rub(1000), rub(700) - 1, "red", 69.9},
		{"макет ТЗ: 3,8 из 5 млн", rub(5000000), rub(3800000), "yellow", 76},
		{"ничего не списано", rub(5000000), 0, "red", 0},
	}
	for _, c := range cases {
		got := Scale(c.planned, c.spent)
		if got.State != c.state || got.Percent != c.percent || got.Known != (c.state != "unknown") {
			t.Errorf("%s: %+v, ожидалось %s %.1f", c.name, got, c.state, c.percent)
		}
	}
	if UniversityNorm(rub(1000)) != rub(700) || UniversityNorm(money.Amount(1)) != 0 {
		t.Fatal("норма вуза — 70% от гранта, вниз до копейки")
	}
}
