package handlers

import (
	"database/sql"
	"encoding/json"
)

// logAudit пишет запись в audit_log. Хранится 2 месяца — см. internal/retention.
func logAudit(db *sql.DB, entityType, entityID, action, userID, comment string, oldVal, newVal interface{}) {
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
	_, _ = db.Exec(
		`INSERT INTO audit_log (entity_type, entity_id, action, user_id, comment_text, old_value, new_value)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entityType, entityIDArg, action, userIDArg, commentArg,
		nullableJSON(oldJSON), nullableJSON(newJSON),
	)
}

func nullableJSON(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
