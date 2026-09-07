// Package retention реализует фоновую очистку по срокам хранения:
//   - журнал аудита (audit_log) хранится 2 месяца (настройка audit_log_retention_days),
//   - подтверждающие документы (attachments) хранятся до retention_expires_at,
//     который вычисляется при загрузке из attachment_retention_days.
package retention

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"time"
)

// Run запускает бесконечный цикл очистки с заданным интервалом. Предполагается
// вызов в отдельной горутине из main().
func Run(db *sql.DB, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	cleanupOnce(db)
	for {
		select {
		case <-ticker.C:
			cleanupOnce(db)
		case <-stop:
			return
		}
	}
}

func cleanupOnce(db *sql.DB) {
	purgeAuditLog(db)
	purgeExpiredAttachments(db)
}

func purgeAuditLog(db *sql.DB) {
	days := 60
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'audit_log_retention_days'`).Scan(&raw); err == nil {
		if v, err2 := strconv.Atoi(raw); err2 == nil {
			days = v
		}
	}
	res, err := db.Exec(`DELETE FROM audit_log WHERE created_at < now() - ($1 || ' days')::interval`, days)
	if err != nil {
		log.Printf("retention: ошибка очистки audit_log: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("retention: удалено %d устаревших записей журнала (> %d дней)", n, days)
	}
}

func purgeExpiredAttachments(db *sql.DB) {
	rows, err := db.Query(`SELECT id, storage_path FROM attachments WHERE retention_expires_at < now()`)
	if err != nil {
		log.Printf("retention: ошибка выборки просроченных вложений: %v", err)
		return
	}
	type toDelete struct{ id, path string }
	var items []toDelete
	for rows.Next() {
		var t toDelete
		if err := rows.Scan(&t.id, &t.path); err == nil {
			items = append(items, t)
		}
	}
	rows.Close()

	for _, it := range items {
		if err := os.Remove(it.path); err != nil && !os.IsNotExist(err) {
			log.Printf("retention: не удалось удалить файл %s: %v", it.path, err)
			continue
		}
		if _, err := db.Exec(`DELETE FROM attachments WHERE id = $1`, it.id); err != nil {
			log.Printf("retention: не удалось удалить запись вложения %s: %v", it.id, err)
		}
	}
	if len(items) > 0 {
		log.Printf("retention: удалено %d просроченных вложений", len(items))
	}
}
