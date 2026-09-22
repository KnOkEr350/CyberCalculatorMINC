package handlers

import "time"

// businessLocation — business timezone инстанса по ADR-02. Используется
// фиксированный UTC+3, как и в остальных обработчиках: образ не несёт tzdata,
// а в Москве нет перехода на летнее время.
var businessLocation = time.FixedZone("Europe/Moscow", 3*60*60)

// calendarDeadline считает регламентный срок Приказа № 270 по ADR-02:
//
//   - срок считается по датам в business timezone, а не сутками от метки
//     времени: важна московская дата получения, а не час;
//   - день получения не включается, первый день срока — следующий;
//   - выходные и праздники не исключаются и не переносят срок;
//   - срок истекает только после окончания последнего календарного дня.
//
// Возвращает последний день срока (YYYY-MM-DD) и момент истечения — начало
// следующего за ним дня по Москве.
func calendarDeadline(receivedAt time.Time, days int) (lastDay string, expiresAt time.Time) {
	year, month, day := receivedAt.In(businessLocation).Date()
	last := time.Date(year, month, day+days, 0, 0, 0, 0, businessLocation)
	return last.Format("2006-01-02"), last.AddDate(0, 0, 1)
}

// deadlineExpired сообщает, истёк ли срок к моменту now: в последний день
// срока до полуночи по Москве срок ещё идёт.
func deadlineExpired(expiresAt, now time.Time) bool {
	return !now.Before(expiresAt)
}
