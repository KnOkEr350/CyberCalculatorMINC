package handlers

import (
	"testing"
	"time"
)

// QA-04: симуляция регламентных таймеров. Календарные сроки Приказа № 270
// считаются по датам, поэтому выходные и праздники их не переносят, високосный
// год и границы месяцев отрабатываются штатно, повторный расчёт даёт тот же
// ответ, а сдвиг часов у вызывающего ничего не меняет.
func TestTimerSimulationCalendarDays(t *testing.T) {
	msk := func(s string) time.Time {
		t.Helper()
		value, err := time.ParseInLocation("2006-01-02 15:04", s, businessLocation)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	t.Run("выходные и праздники не переносят срок", func(t *testing.T) {
		// 1 января 2027 — пятница и нерабочий день: срок всё равно истекает.
		lastDay, expiresAt := calendarDeadline(msk("2026-12-22 10:00"), 10)
		if lastDay != "2027-01-01" {
			t.Fatalf("последний день %s, ожидалось 2027-01-01", lastDay)
		}
		if deadlineExpired(expiresAt, msk("2027-01-01 23:59")) {
			t.Fatal("в нерабочий день срок ещё идёт до полуночи")
		}
		if !deadlineExpired(expiresAt, msk("2027-01-02 00:00")) {
			t.Fatal("после праздничного последнего дня срок истёк")
		}
	})

	t.Run("високосный год", func(t *testing.T) {
		// 2028 — високосный: 29 февраля существует и участвует в счёте.
		if lastDay, _ := calendarDeadline(msk("2028-02-24 10:00"), 5); lastDay != "2028-02-29" {
			t.Fatalf("последний день %s, ожидалось 2028-02-29", lastDay)
		}
		// 2027 — невисокосный: те же пять дней дают 1 марта.
		if lastDay, _ := calendarDeadline(msk("2027-02-24 10:00"), 5); lastDay != "2027-03-01" {
			t.Fatalf("последний день %s, ожидалось 2027-03-01", lastDay)
		}
	})

	t.Run("повторный расчёт идемпотентен", func(t *testing.T) {
		received := msk("2026-11-10 09:30")
		firstDay, firstExpiry := calendarDeadline(received, 20)
		for i := 0; i < 5; i++ {
			day, expiry := calendarDeadline(received, 20)
			if day != firstDay || !expiry.Equal(firstExpiry) {
				t.Fatalf("повторный расчёт дал другой срок: %s/%s против %s/%s", day, expiry, firstDay, firstExpiry)
			}
		}
	})

	t.Run("сдвиг часового пояса вызывающего ничего не меняет", func(t *testing.T) {
		received := msk("2026-11-10 09:30")
		want, wantExpiry := calendarDeadline(received, 20)
		for _, offset := range []int{-10, -5, 0, 3, 9, 12} {
			zone := time.FixedZone("test", offset*3600)
			day, expiry := calendarDeadline(received.In(zone), 20)
			if day != want || !expiry.Equal(wantExpiry) {
				t.Fatalf("смещение %+d: срок %s, ожидалось %s", offset, day, want)
			}
		}
	})

	t.Run("сроки предварительного и итогового перечня отличаются", func(t *testing.T) {
		received := msk("2026-11-10 12:00")
		preliminary, _ := calendarDeadline(received, 10)
		final, _ := calendarDeadline(received, 20)
		if preliminary != "2026-11-20" {
			t.Fatalf("предварительный перечень: %s, ожидалось 2026-11-20", preliminary)
		}
		if final != "2026-11-30" {
			t.Fatalf("итоговый перечень: %s, ожидалось 2026-11-30", final)
		}
	})

	t.Run("срок не истекает раньше своего последнего дня", func(t *testing.T) {
		_, expiresAt := calendarDeadline(msk("2026-09-10 15:00"), 10)
		for _, moment := range []string{"2026-09-10 15:00", "2026-09-15 12:00", "2026-09-20 00:00", "2026-09-20 23:59"} {
			if deadlineExpired(expiresAt, msk(moment)) {
				t.Fatalf("в момент %s срок ещё не истёк", moment)
			}
		}
		if !deadlineExpired(expiresAt, msk("2026-09-21 00:00")) {
			t.Fatal("с началом следующего дня срок истекает")
		}
	})
}
