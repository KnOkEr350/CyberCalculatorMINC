package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
)

type auditExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// logAudit пишет запись в audit_log. Возвращаемая ошибка позволяет включать
// запись журнала в ту же транзакцию, что и изменение предметных данных.
func logAudit(db auditExecer, entityType, entityID, action, userID, comment string, oldVal, newVal interface{}) error {
	var oldJSON, newJSON []byte
	var err error
	if oldVal != nil {
		oldJSON, err = json.Marshal(oldVal)
		if err != nil {
			return err
		}
	}
	if newVal != nil {
		newJSON, err = json.Marshal(newVal)
		if err != nil {
			return err
		}
	}
	var entityIDArg interface{}
	if entityID != "" {
		entityIDArg = entityID
	}
	var commentArg interface{}
	if comment != "" {
		commentArg = comment
	}
	var userIDArg interface{}
	if userID != "" {
		userIDArg = userID
	}
	_, err = db.Exec(
		`INSERT INTO audit_log (entity_type, entity_id, action, user_id, comment_text, old_value, new_value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entityType, entityIDArg, action, userIDArg, commentArg,
		nullableJSON(oldJSON), nullableJSON(newJSON),
	)
	if err != nil {
		slog.Error("audit write failed", "entity_type", entityType, "action", action)
	}
	return err
}

func nullableJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
