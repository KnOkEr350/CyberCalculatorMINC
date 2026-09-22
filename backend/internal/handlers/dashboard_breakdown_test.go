package handlers

import (
	"testing"

	"cybercalc/internal/money"
)

// DASH-04: обязательность вида зависит от аудитории. Справочник помечает
// Виды 1 и 3 обязательными, но по ТЗ (§7.1) «для СПО обязательных видов
// мероприятий нет», а школьный трек вариативен целиком.
func TestEffectiveObligationDependsOnAudience(t *testing.T) {
	cases := []struct {
		dictionary, audience, want string
	}{
		{"mandatory", "vuz", "mandatory"},
		{"mandatory", "kolledj", "variable"}, // СПО: обязательных видов нет
		{"mandatory", "school", "variable"},
		{"variable", "vuz", "variable"},
		{"variable", "kolledj", "variable"},
		{"variable", "school", "variable"},
	}
	for _, tc := range cases {
		if got := effectiveObligation(tc.dictionary, tc.audience); got != tc.want {
			t.Fatalf("вид %q для аудитории %q: обязательность %q, ожидалась %q",
				tc.dictionary, tc.audience, got, tc.want)
		}
	}
}

// DASH-05: «Скрыть нулевые позиции» из макета экрана 1. Флаг включается
// явно, иначе данные не прячутся.
func TestHideZeroPositionsFlag(t *testing.T) {
	for _, value := range []string{"1", "true", "TRUE", "yes", "on", " on "} {
		if !hideZeroPositions(value) {
			t.Fatalf("значение %q должно включать скрытие нулевых позиций", value)
		}
	}
	for _, value := range []string{"", "0", "false", "no", "нет", "null"} {
		if hideZeroPositions(value) {
			t.Fatalf("значение %q не должно скрывать данные", value)
		}
	}
}

func TestWithoutZeroPositionsKeepsMeaningfulRows(t *testing.T) {
	rows := []categoryBreakdown{
		{CategoryCode: "teachers", Audience: "vuz", AmountRub: money.Amount(100000), UnitCount: 14},
		// Объём есть, затраты ещё не подтверждены — позиция не пустая.
		{CategoryCode: "ood_rpd", Audience: "vuz", AmountRub: 0, UnitCount: 2},
		// Ни суммы, ни объёма — пустая позиция.
		{CategoryCode: "edu_content", Audience: "school", AmountRub: 0, UnitCount: 0},
	}
	got := withoutZeroPositions(rows)
	if len(got) != 2 {
		t.Fatalf("ожидали две непустые позиции, получили %d: %+v", len(got), got)
	}
	for _, row := range got {
		if row.CategoryCode == "edu_content" {
			t.Fatalf("пустая позиция не удалена: %+v", row)
		}
	}
	// Без флага ничего не теряется.
	if len(rows) != 3 {
		t.Fatal("исходный срез не должен изменяться")
	}
}
