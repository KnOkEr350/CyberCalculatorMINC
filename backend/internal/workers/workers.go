// Package workers owns the lifecycle of scheduled maintenance and directory
// synchronization jobs. Keeping it outside the HTTP process lets API replicas
// scale independently and prevents rolling web deployments from restarting
// long-running maintenance work.
package workers

import (
	"context"
	"database/sql"
	"log"
	"os"
	"sync"
	"time"

	"cybercalc/internal/config"
	"cybercalc/internal/curators"
	"cybercalc/internal/handlers"
	"cybercalc/internal/regulatory"
	"cybercalc/internal/retention"
	"cybercalc/internal/tasks"
)

// Run starts all background loops and blocks until ctx is cancelled. Database
// advisory locks inside directory jobs remain the cross-process safety net for
// manual commands and accidental duplicate workers.
func Run(ctx context.Context, db *sql.DB, cfg config.Config) error {
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		return err
	}

	stop := make(chan struct{})
	var group sync.WaitGroup
	start := func(run func()) {
		group.Add(1)
		go func() {
			defer group.Done()
			run()
		}()
	}

	start(func() { retention.Run(db, time.Hour, stop, cfg.UploadDir) })
	// Согласование по молчанию: срок истекает в полночь по Москве, поэтому
	// проверка идёт каждые 15 минут, и переход не опаздывает больше чем на них.
	start(func() { regulatory.RunExpiry(db, 15*time.Minute, stop) })
	// Закрепления кураторов начинаются и кончаются по датам: кэш users.partner_id
	// догоняет их раз в 15 минут (доступ при этом проверяется по датам сразу).
	start(func() { curators.RunSync(db, 15*time.Minute, stop) })
	// Задачи, ушедшие по fallback, возвращаются к специалисту, когда он
	// появился, и уходят выше, когда куратор перестал быть закреплённым.
	start(func() { tasks.RunRedispatch(db, 15*time.Minute, stop) })
	start(func() {
		handlers.RunDirectorySync(db, cfg.DirectorySyncURL, time.Duration(cfg.DirectorySyncHours)*time.Hour, stop)
	})
	if cfg.DirectoryEnrichOnStart {
		start(func() {
			handlers.RunDirectoryPrograms(db, cfg.DirectoryEnrichLimit, time.Duration(cfg.DirectorySyncHours)*time.Hour, stop)
		})
		start(func() {
			result, err := handlers.EnrichEducationDirectory(ctx, db, cfg.DirectoryEnrichLimit, time.Duration(cfg.DirectoryEnrichDelayMS)*time.Millisecond)
			if err != nil && ctx.Err() == nil {
				log.Printf("автозаполнение ИНН/ОГРН остановлено после %d записей: %v", result.Processed, err)
				return
			}
			if ctx.Err() != nil {
				return
			}
			log.Printf("автозаполнение ИНН/ОГРН завершено: обработано %d, найдено %d, без результата %d", result.Processed, result.Matched, result.Unmatched)
		})
	}

	log.Print("фоновые задания запущены")
	<-ctx.Done()
	close(stop)
	group.Wait()
	log.Print("фоновые задания остановлены")
	return nil
}
