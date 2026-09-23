package regulatory

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// RunExpiry запускает фоновое согласование по молчанию (ТЗ §6: cron-задача,
// проверяющая даты отправки). Проход выполняется сразу и затем по интервалу;
// остановка — закрытием stop. Несколько экземпляров задания безопасны:
// процесс блокируется на время перехода, а повторный проход ничего не меняет.
func RunExpiry(db *sql.DB, interval time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pass := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		approved, lapsed, err := ExpireDue(ctx, db, time.Now())
		if err != nil {
			log.Printf("регламентные сроки: %v", err)
			return
		}
		if approved > 0 || lapsed > 0 {
			log.Printf("регламентные сроки: согласовано по молчанию %d, пропущено доработок %d", approved, lapsed)
		}
	}
	pass()
	for {
		select {
		case <-ticker.C:
			pass()
		case <-stop:
			return
		}
	}
}
