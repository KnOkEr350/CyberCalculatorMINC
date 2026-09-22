package handlers

import (
	"testing"
	"time"
)

// WF-02 / ADR-02: сроки Приказа № 270 считаются календарными днями по
// московским датам; день получения не входит; выходные не переносят срок;
// просрочка наступает только после окончания последнего дня.
func TestCalendarDeadlineBoundaries(t *testing.T) {
	msk := func(s string) time.Time {
		t.Helper()
		v, err := time.ParseInLocation("2006-01-02 15:04", s, businessLocation)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name       string
		receivedAt time.Time
		days       int
		lastDay    string
	}{
		{"10 дней: день получения не входит", msk("2026-09-10 10:00"), 10, "2026-09-20"},
		{"получено поздно вечером — считается та же московская дата", msk("2026-09-10 23:59"), 10, "2026-09-20"},
		// 22:30 UTC 10 сентября — это уже 01:30 11 сентября по Москве.
		{"дата берётся по Москве, а не по UTC", time.Date(2026, 9, 10, 22, 30, 0, 0, time.UTC), 10, "2026-09-21"},
		{"20 дней итогового перечня", msk("2027-02-25 12:00"), 20, "2027-03-17"},
		{"5 дней повторного рассмотрения через конец месяца", msk("2026-11-28 09:00"), 5, "2026-12-03"},
		{"переход через Новый год", msk("2026-12-25 15:00"), 10, "2027-01-04"},
		{"високосный февраль", msk("2028-02-25 15:00"), 5, "2028-03-01"},
		// 2026-09-19 — суббота: выходной не переносит окончание срока.
		{"окончание в выходной не переносится", msk("2026-09-09 11:00"), 10, "2026-09-19"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastDay, expiresAt := calendarDeadline(tc.receivedAt, tc.days)
			if lastDay != tc.lastDay {
				t.Fatalf("последний день = %s, want %s", lastDay, tc.lastDay)
			}
			wantExpiry := msk(tc.lastDay+" 00:00").AddDate(0, 0, 1)
			if !expiresAt.Equal(wantExpiry) {
				t.Fatalf("истечение = %s, want начало следующего дня по Москве %s", expiresAt, wantExpiry)
			}
		})
	}
}

func TestDeadlineExpiresOnlyAfterLastCalendarDay(t *testing.T) {
	received := time.Date(2026, 9, 10, 15, 0, 0, 0, businessLocation)
	_, expiresAt := calendarDeadline(received, 10) // последний день — 20.09.2026
	at := func(s string) time.Time {
		v, _ := time.ParseInLocation("2006-01-02 15:04:05", s, businessLocation)
		return v
	}
	// Прежний расчёт (метка + 10 суток) объявлял просрочку уже в 15:00
	// последнего дня срока.
	if deadlineExpired(expiresAt, at("2026-09-20 15:00:01")) {
		t.Fatal("в последний день срока днём просрочки ещё нет")
	}
	if deadlineExpired(expiresAt, at("2026-09-20 23:59:59")) {
		t.Fatal("за секунду до полуночи последнего дня срок ещё идёт")
	}
	if !deadlineExpired(expiresAt, at("2026-09-21 00:00:00")) {
		t.Fatal("с полуночи следующего дня срок истёк")
	}
}

// TOP-10 / ADR-03: для освобождения по п. 22 в иной ОО достаточно
// обязательных Видов 1 и 3; вариативные виды не требуются.
func TestClause22RequiresOnlyMandatoryActivities(t *testing.T) {
	cases := []struct {
		teachers, programs, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	}
	for _, tc := range cases {
		if got := clause22AlternativeSatisfied(tc.teachers, tc.programs); got != tc.want {
			t.Fatalf("Вид 1=%v, Вид 3=%v: got %v, want %v", tc.teachers, tc.programs, got, tc.want)
		}
	}
}
