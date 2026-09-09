// Package retention реализует фоновую очистку по срокам хранения:
//   - журнал аудита (audit_log) хранится 2 месяца (настройка audit_log_retention_days),
//   - подтверждающие документы (attachments) хранятся до retention_expires_at,
//     который вычисляется при загрузке из attachment_retention_days.
package retention

import (
	"context"
	"cybercalc/internal/filestore"
	"database/sql"
	"log"
	"os"
	"time"
)

// Run запускает бесконечный цикл очистки с заданным интервалом. Предполагается
// вызов в отдельной горутине из main().
func Run(db *sql.DB, interval time.Duration, stop <-chan struct{}, root ...string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	uploadRoot := "/data/uploads"
	if len(root) > 0 {
		uploadRoot = root[0]
	}
	cleanupOnce(db, uploadRoot)
	for {
		select {
		case <-ticker.C:
			cleanupOnce(db, uploadRoot)
		case <-stop:
			return
		}
	}
}

func cleanupOnce(db *sql.DB, root string) {
	purgeAuditLog(db)
	purgeExpiredAttachments(db, root)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < now()`); err != nil {
		log.Printf("retention sessions: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM auth_rate_limits WHERE expires_at < now()`); err != nil {
		log.Printf("retention rate limits: %v", err)
	}
}

func purgeAuditLog(db *sql.DB) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var n int64
	err := db.QueryRowContext(ctx, `SELECT purge_expired_audit()`).Scan(&n)
	if err != nil {
		log.Printf("retention: ошибка очистки audit_log: %v", err)
		return
	}
	if n > 0 {
		log.Printf("retention: удалено %d устаревших записей журнала", n)
	}
}

func purgeExpiredAttachments(db *sql.DB, root string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// The database trigger queues disk deletion atomically with metadata removal.
	if _, err := db.ExecContext(ctx, `DELETE FROM attachments WHERE id IN (SELECT id FROM attachments WHERE retention_expires_at<now() LIMIT 500)`); err != nil {
		log.Printf("retention metadata: %v", err)
		return
	}
	for i := 0; i < 500; i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		var id int64
		var path string
		err = tx.QueryRowContext(ctx, `SELECT id,storage_path FROM file_deletion_queue ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &path)
		if err != nil {
			tx.Rollback()
			return
		}
		if err := filestore.Remove(root, path); err != nil && !os.IsNotExist(err) {
			tx.Rollback()
			log.Printf("retention: file deletion failed for job %d", id)
			return
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_deletion_queue WHERE id=$1`, id); err != nil {
			tx.Rollback()
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("retention commit: %v", err)
			return
		}
	}
}
