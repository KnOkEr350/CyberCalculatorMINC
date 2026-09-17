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
	"cybercalc/internal/handlers"
	"cybercalc/internal/retention"
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
