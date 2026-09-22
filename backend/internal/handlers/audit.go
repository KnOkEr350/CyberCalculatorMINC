package handlers

import (
	"context"
	"log/slog"

	platformaudit "cybercalc/internal/platform/audit"
)

// logAudit пишет запись в audit_log. Возвращаемая ошибка позволяет включать
// запись журнала в ту же транзакцию, что и изменение предметных данных.
func logAudit(ctx context.Context, db platformaudit.Execer, entityType, entityID, action, userID, comment string, oldVal, newVal interface{}) error {
	err := platformaudit.Write(ctx, db, platformaudit.Event{
		Actor:   platformaudit.UserActor(userID),
		Action:  action,
		Entity:  platformaudit.Entity{Type: entityType, ID: entityID},
		Before:  oldVal,
		After:   newVal,
		Comment: comment,
	})
	if err != nil {
		slog.Error("audit write failed", "entity_type", entityType, "action", action)
	}
	return err
}
