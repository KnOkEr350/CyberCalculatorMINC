package handlers

import (
	"database/sql"
	"encoding/json"
)

type auditExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// logAudit пишет запись в audit_log. Возвращаемая ошибка позволяет включать
// запись журнала в ту же транзакцию, что и изменение предметных данных.
func logAudit(db auditExecer, entityType, entityID, action, userID, comment string, oldVal, newVal interface{}) error {
	var oldJSON, newJSON []byte
	if oldVal != nil {
		oldJSON, _ = json.Marshal(oldVal)
	}
	if newVal != nil {
		newJSON, _ = json.Marshal(newVal)
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
	_, err := db.Exec(
		`INSERT INTO audit_log (entity_type, entity_id, action, user_id, comment_text, old_value, new_value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entityType, entityIDArg, action, userIDArg, commentArg,
		nullableJSON(oldJSON), nullableJSON(newJSON),
	)
	return err
}

func nullableJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
