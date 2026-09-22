package handlers

import (
	"testing"

	"cybercalc/internal/money"
)

// DASH-01: выполнение норматива 3% считается точной денежной арифметикой,
// дефицит и профицит не смешиваются — ненулевым бывает ровно один из них, а
// процент выполнения норматива отделён от процента реализации плана.
func TestTargetProgressSeparatesDeficitAndSurplus(t *testing.T) {
	cases := []struct {
		name              string
		target, confirmed money.Amount
		wantDeficit       money.Amount
		wantSurplus       money.Amount
		wantCompletionPct float64
	}{
		{"норматив не выполнен", money.Amount(1_000_000_00), money.Amount(600_000_00), money.Amount(400_000_00), 0, 60},
		{"норматив выполнен ровно", money.Amount(1_000_000_00), money.Amount(1_000_000_00), 0, 0, 100},
		{"норматив перевыполнен", money.Amount(1_000_000_00), money.Amount(1_250_000_00), 0, money.Amount(250_000_00), 125},
		{"подтверждённого факта нет", money.Amount(1_000_000_00), 0, money.Amount(1_000_000_00), 0, 0},
		{"норматив не задан", 0, money.Amount(500_00), 0, money.Amount(500_00), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deficit, surplus, completion := targetProgress(tc.target, tc.confirmed)
			if deficit != tc.wantDeficit {
				t.Fatalf("дефицит %s ₽, ожидалось %s ₽", deficit, tc.wantDeficit)
			}
			if surplus != tc.wantSurplus {
				t.Fatalf("профицит %s ₽, ожидалось %s ₽", surplus, tc.wantSurplus)
			}
			if completion != tc.wantCompletionPct {
				t.Fatalf("процент выполнения %v, ожидалось %v", completion, tc.wantCompletionPct)
			}
			if deficit > 0 && surplus > 0 {
				t.Fatalf("дефицит и профицит не могут быть ненулевыми одновременно: %s / %s", deficit, surplus)
			}
		})
	}
}

// Копейки не теряются: расчёт ведётся в копейках, а не в рублях с плавающей
// точкой.
func TestTargetProgressKeepsKopecks(t *testing.T) {
	target := money.Amount(100_000_01)    // 100 000,01 ₽
	confirmed := money.Amount(100_000_00) // 100 000,00 ₽
	deficit, surplus, _ := targetProgress(target, confirmed)
	if deficit != money.Amount(1) {
		t.Fatalf("дефицит %s ₽, ожидалась ровно одна копейка", deficit)
	}
	if surplus != 0 {
		t.Fatalf("профицита быть не должно: %s ₽", surplus)
	}
}
