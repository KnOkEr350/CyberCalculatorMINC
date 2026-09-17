package handlers

import (
	"testing"

	"cybercalc/internal/money"
)

func TestResolveEntryAmount(t *testing.T) {
	formula := money.Amount(12_345)
	actual := money.Amount(54_321)

	method, amount, err := resolveEntryAmount("", nil, formula)
	if err != nil || method != "average" || amount != formula {
		t.Fatalf("default method: method=%q amount=%v err=%v", method, amount, err)
	}

	method, amount, err = resolveEntryAmount("actual", &actual, formula)
	if err != nil || method != "actual" || amount != actual {
		t.Fatalf("actual method: method=%q amount=%v err=%v", method, amount, err)
	}

	for name, method := range map[string]string{
		"actual without amount": "actual",
		"unknown method":        "manual",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := resolveEntryAmount(method, nil, formula); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	if _, _, err := resolveEntryAmount("average", &actual, formula); err == nil {
		t.Fatal("average method must reject an actual amount")
	}
}
