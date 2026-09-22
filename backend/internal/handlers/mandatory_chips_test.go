package handlers

import (
	"strings"
	"testing"

	"cybercalc/internal/money"
)

// DASH-03: обязательный минимум ВО считается только по аудитории высшего
// образования. Раньше чипы собирались на фронте по суммам всех аудиторий,
// поэтому мероприятие для СПО закрывало минимум, обязательный для вузов.
func TestHigherEducationChipsIgnoreNonUniversityAudiences(t *testing.T) {
	fact := []categoryBreakdown{
		// Преподаватели есть, но только в колледже — минимум ВО не закрыт.
		{CategoryCode: "teachers", Audience: "kolledj", AmountRub: money.Amount(50000000)},
		// ООП/РПД подтверждены в вузе — этот минимум закрыт.
		{CategoryCode: "ood_rpd", Audience: "vuz", AmountRub: money.Amount(203985000)},
	}
	chips := map[string]mandatoryChip{}
	for _, chip := range higherEducationChips(fact) {
		chips[chip.CategoryCode] = chip
	}
	if len(chips) != 3 {
		t.Fatalf("ожидали чипы Видов 1, 3 и альтернативы ТОП-ИТ, получили %d", len(chips))
	}
	if chips["teachers"].Complete {
		t.Fatalf("мероприятие для СПО не закрывает минимум ВО: %+v", chips["teachers"])
	}
	if chips["teachers"].AmountRub != 0 {
		t.Fatalf("в сумму минимума ВО попали затраты другой аудитории: %v", chips["teachers"].AmountRub)
	}
	if !chips["ood_rpd"].Complete || chips["ood_rpd"].AmountRub != money.Amount(203985000) {
		t.Fatalf("подтверждённый вуз должен закрывать минимум: %+v", chips["ood_rpd"])
	}
	if chips["top_it"].Complete {
		t.Fatalf("без затрат ТОП-ИТ альтернатива не применяется: %+v", chips["top_it"])
	}
}

// Каждый чип обязан объяснять своё правило, а альтернатива ТОП-ИТ — условие
// пункта 22 Порядка, иначе пользователь не поймёт, почему минимум закрыт.
func TestHigherEducationChipsExplainTheirRule(t *testing.T) {
	for _, chip := range higherEducationChips(nil) {
		if strings.TrimSpace(chip.Explanation) == "" {
			t.Fatalf("чип %q без объяснения", chip.CategoryCode)
		}
		if chip.Complete {
			t.Fatalf("без фактических затрат чип %q не может быть закрыт", chip.CategoryCode)
		}
	}
	var top mandatoryChip
	for _, chip := range higherEducationChips(nil) {
		if chip.CategoryCode == "top_it" {
			top = chip
		}
	}
	if !top.Alternative {
		t.Fatal("ТОП-ИТ должен быть помечен как альтернатива, а не как обязательный вид")
	}
	if !strings.Contains(top.Explanation, "22") || !strings.Contains(top.Explanation, "другой образовательной организации") {
		t.Fatalf("объяснение альтернативы не раскрывает условие пункта 22: %q", top.Explanation)
	}
}

// Суммы одного вида по нескольким строкам вуза складываются: разбивка
// приходит по парам «вид + аудитория».
func TestHigherEducationChipsSumRowsOfSameCategory(t *testing.T) {
	fact := []categoryBreakdown{
		{CategoryCode: "teachers", Audience: "vuz", AmountRub: money.Amount(100000)},
		{CategoryCode: "teachers", Audience: "vuz", AmountRub: money.Amount(150000)},
	}
	for _, chip := range higherEducationChips(fact) {
		if chip.CategoryCode != "teachers" {
			continue
		}
		if chip.AmountRub != money.Amount(250000) || !chip.Complete {
			t.Fatalf("суммы строк одного вида должны складываться: %+v", chip)
		}
	}
}
