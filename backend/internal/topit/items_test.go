package topit

import (
	"errors"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/money"
)

var today = time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

func amount(s string) *money.Amount {
	v, err := money.Parse(s)
	if err != nil {
		panic(err)
	}
	return &v
}

func support() Input {
	return Input{Kind: "support", Title: "Сервер для лаборатории", SupportKind: "equipment", ActReference: "Акт № 12",
		ActDate: "2026-05-01", BalanceValueRub: amount("500000"), ConfirmedValueRub: amount("450000")}
}

func scholarship() Input {
	return Input{Kind: "scholarship", Title: "Именная стипендия", StudentName: "Иванов Иван", GroupName: "ИВТ-31", Course: 3,
		PeriodStart: "2026-09-01", PeriodEnd: "2027-01-31", AmountRub: amount("120000"), Criterion: "Средний балл не ниже 4,5", DonorName: "ООО «Пример»"}
}

func caseInput() Input {
	return Input{Kind: "case", Title: "Оптимизация логистики", ImplementationOrg: "ООО «Пример»", ImplementationStatus: "implemented",
		ImplementedOn: "2026-06-15", Description: "Внедрена модель маршрутизации"}
}

func TestValidRowsOfEveryKindAreAccepted(t *testing.T) {
	for name, in := range map[string]Input{"поддержка": support(), "стипендия": scholarship(), "кейс": caseInput()} {
		if _, err := Normalize(in, today); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestRowsAreRejectedWhenIncompleteOrInconsistent(t *testing.T) {
	mutate := func(in Input, edit func(*Input)) Input { edit(&in); return in }
	cases := map[string]Input{
		"неизвестный вид":                mutate(support(), func(i *Input) { i.Kind = "grant" }),
		"короткое название":              mutate(support(), func(i *Input) { i.Title = "А" }),
		"вид поддержки":                  mutate(support(), func(i *Input) { i.SupportKind = "money" }),
		"поддержка без акта":             mutate(support(), func(i *Input) { i.ActReference = "" }),
		"акт из будущего":                mutate(support(), func(i *Input) { i.ActDate = "2026-12-01" }),
		"поддержка без стоимости":        mutate(support(), func(i *Input) { i.BalanceValueRub, i.AppraisedValueRub, i.ConfirmedValueRub = nil, nil, nil }),
		"подтверждённая выше балансовой": mutate(support(), func(i *Input) { i.ConfirmedValueRub = amount("500000.01") }),
		"поле стипендии у поддержки":     mutate(support(), func(i *Input) { i.StudentName = "Иванов" }),
		"стипендия: курс":                mutate(scholarship(), func(i *Input) { i.Course = 7 }),
		"стипендия: период":              mutate(scholarship(), func(i *Input) { i.PeriodEnd = "2026-08-31" }),
		"стипендия: нулевая сумма":       mutate(scholarship(), func(i *Input) { i.AmountRub = amount("0") }),
		"стипендия: без донора":          mutate(scholarship(), func(i *Input) { i.DonorName = "" }),
		"кейс: неизвестный статус":       mutate(caseInput(), func(i *Input) { i.ImplementationStatus = "done" }),
		"кейс: внедрён без даты":         mutate(caseInput(), func(i *Input) { i.ImplementedOn = "" }),
		"кейс: предложен с датой":        mutate(caseInput(), func(i *Input) { i.ImplementationStatus = "proposed" }),
		"кейс: внедрение в будущем":      mutate(caseInput(), func(i *Input) { i.ImplementedOn = "2027-01-01" }),
		"кейс с полем поддержки":         mutate(caseInput(), func(i *Input) { i.ActReference = "акт" }),
		"документ дважды":                mutate(support(), func(i *Input) { i.DocumentIDs = []string{"a", "a"} }),
	}
	for name, in := range cases {
		if _, err := Normalize(in, today); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: строка должна отвергаться, получено %v", name, err)
		}
	}
}

func TestTextIsNormalised(t *testing.T) {
	in := scholarship()
	in.StudentName = "  Иванов   Иван "
	got, err := Normalize(in, today)
	if err != nil || got.StudentName != "Иванов Иван" {
		t.Fatalf("пробелы схлопываются: %q %v", got.StudentName, err)
	}
	long := support()
	long.Title = strings.Repeat("я", 301)
	if _, err := Normalize(long, today); err == nil {
		t.Fatal("слишком длинное название")
	}
}

func TestSummaryKeepsUnconfirmedSupportOutOfTheTotal(t *testing.T) {
	items := []Item{
		{Input: support()},
		{Input: Input{Kind: "support", BalanceValueRub: amount("100")}},
		{Input: scholarship()}, {Input: scholarship()},
		{Input: caseInput()}, {Input: Input{Kind: "case", ImplementationStatus: "proposed"}},
	}
	s, err := Summarize(items)
	if err != nil {
		t.Fatal(err)
	}
	if s.Supports != 2 || s.UnconfirmedSupports != 1 || s.ConfirmedSupportRub.String() != "450000.00" ||
		s.Scholarships != 2 || s.ScholarshipsTotalRub.String() != "240000.00" || s.Cases != 2 || s.ImplementedCases != 1 {
		t.Fatalf("сводка: %+v", s)
	}
}
