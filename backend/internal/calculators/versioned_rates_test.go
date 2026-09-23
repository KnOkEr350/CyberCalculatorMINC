package calculators

import (
	"encoding/json"
	"testing"

	"cybercalc/internal/models"
	"cybercalc/internal/money"
)

func TestCalculateAmountWithVersionedRate(t *testing.T) {
	got, err := CalculateAmountWithRates("teachers", models.AudienceVuz,
		map[string]interface{}{"academic_hours": json.Number("0.5")},
		map[string]string{"academic_hour": "5000.25"})
	if err != nil {
		t.Fatal(err)
	}
	if want := money.Amount(250013); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestCalculateAmountWithRatesFailsClosed(t *testing.T) {
	_, err := CalculateAmountWithRates("teachers", models.AudienceVuz,
		map[string]interface{}{"academic_hours": json.Number("1")}, nil)
	if err == nil {
		t.Fatal("missing versioned rates must fail")
	}
}
