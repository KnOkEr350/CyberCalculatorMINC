package handlers

import (
	"testing"
	"time"
)

// WF-05 / ADR-02: контрольные даты Приказа № 270 считаются по московским
// датам. Раньше срок и остаток дней вычислялись в браузере по локальному
// времени, поэтому в другом часовом поясе показывались другие сутки.
func TestRegulatoryMilestonesDates(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, businessLocation)
	byCode := map[string]regulatoryMilestone{}
	for _, milestone := range regulatoryMilestones(2026, now) {
		byCode[milestone.Code] = milestone
	}
	want := map[string]string{
		"budget_notified":    "2026-07-31",
		"epgu_published":     "2026-03-15",
		"preliminary_cutoff": "2026-11-01",
		"preliminary_sent":   "2026-11-10",
		"preliminary_report": "2026-12-10",
		"final_cutoff":       "2026-12-31",
		"final_sent":         "2027-03-01", // итоговый перечень — до 1 марта следующего года
	}
	if len(byCode) != len(want) {
		t.Fatalf("контрольных дат %d, ожидалось %d", len(byCode), len(want))
	}
	for code, date := range want {
		milestone, ok := byCode[code]
		if !ok {
			t.Fatalf("нет контрольной даты %s", code)
		}
		if milestone.Date != date {
			t.Fatalf("%s: дата %s, ожидалась %s", code, milestone.Date, date)
		}
		if milestone.Label == "" || milestone.Basis == "" {
			t.Fatalf("%s: пустая подпись или основание: %+v", code, milestone)
		}
	}
	// 22 сентября 2026: 31 июля прошло, до 1 ноября — 40 дней.
	if m := byCode["budget_notified"]; !m.Overdue || m.DaysLeft >= 0 {
		t.Fatalf("прошедшая дата должна быть просрочена: %+v", m)
	}
	if m := byCode["preliminary_cutoff"]; m.Overdue || m.DaysLeft != 40 {
		t.Fatalf("до 1 ноября должно оставаться 40 дней: %+v", m)
	}
}

func TestRegulatoryMilestonesCountDaysByMoscowDate(t *testing.T) {
	// 31 октября 23:30 по Москве — это 20:30 UTC. Срок 1 ноября наступает
	// завтра, а не сегодня, независимо от часового пояса вызывающего.
	moscowEvening := time.Date(2026, 10, 31, 23, 30, 0, 0, businessLocation)
	find := func(now time.Time) regulatoryMilestone {
		for _, milestone := range regulatoryMilestones(2026, now) {
			if milestone.Code == "preliminary_cutoff" {
				return milestone
			}
		}
		t.Fatal("контрольная дата не найдена")
		return regulatoryMilestone{}
	}
	if m := find(moscowEvening); m.DaysLeft != 1 || m.Overdue {
		t.Fatalf("вечером 31 октября до среза должен оставаться 1 день: %+v", m)
	}
	if m := find(moscowEvening.UTC()); m.DaysLeft != 1 {
		t.Fatalf("тот же момент в UTC должен дать тот же остаток: %+v", m)
	}
	// Наступил 1 ноября по Москве — срок сегодня, не просрочен.
	if m := find(moscowEvening.Add(31 * time.Minute)); m.DaysLeft != 0 || m.Overdue {
		t.Fatalf("1 ноября срок наступает сегодня и не просрочен: %+v", m)
	}
}
