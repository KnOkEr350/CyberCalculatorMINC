// Package regulatory реализует регламентный процесс Приказа № 270: статусы,
// цепочки сроков рассмотрения и согласование по молчанию (WF-01, WF-03, WF-04,
// WF-05). Сроки календарные и считаются по датам в business timezone (ADR-02).
package regulatory

import "time"

// BusinessLocation — business timezone инстанса по ADR-02. Используется
// фиксированный UTC+3: образ не несёт tzdata, а в Москве нет перехода на
// летнее время.
var BusinessLocation = time.FixedZone("Europe/Moscow", 3*60*60)

// CalendarDeadline считает регламентный срок по ADR-02:
//
//   - срок считается по датам в business timezone, а не сутками от метки
//     времени: важна московская дата получения, а не час;
//   - день получения не включается, первый день срока — следующий;
//   - выходные и праздники не исключаются и не переносят срок;
//   - срок истекает только после окончания последнего календарного дня.
//
// Возвращает последний день срока (YYYY-MM-DD) и момент истечения — начало
// следующего за ним дня по Москве.
func CalendarDeadline(receivedAt time.Time, days int) (lastDay string, expiresAt time.Time) {
	year, month, day := receivedAt.In(BusinessLocation).Date()
	last := time.Date(year, month, day+days, 0, 0, 0, 0, BusinessLocation)
	return last.Format("2006-01-02"), last.AddDate(0, 0, 1)
}

// DeadlineExpired сообщает, истёк ли срок к моменту now: в последний день
// срока до полуночи по Москве срок ещё идёт.
func DeadlineExpired(expiresAt, now time.Time) bool {
	return !now.Before(expiresAt)
}

// Today возвращает московскую дату момента now как полночь этой даты.
func Today(now time.Time) time.Time {
	year, month, day := now.In(BusinessLocation).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, BusinessLocation)
}
