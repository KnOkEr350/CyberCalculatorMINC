package handlers

import (
	"time"

	"cybercalc/internal/regulatory"
)

// businessLocation — business timezone инстанса по ADR-02. Определение живёт в
// пакете regulatory: там же считаются сроки процесса, и второй копии правила
// быть не должно.
var businessLocation = regulatory.BusinessLocation

// calendarDeadline и deadlineExpired — тонкие обёртки для обработчиков; правила
// счёта (ADR-02) и их граничные тесты находятся в пакете regulatory.
func calendarDeadline(receivedAt time.Time, days int) (lastDay string, expiresAt time.Time) {
	return regulatory.CalendarDeadline(receivedAt, days)
}

func deadlineExpired(expiresAt, now time.Time) bool {
	return regulatory.DeadlineExpired(expiresAt, now)
}
