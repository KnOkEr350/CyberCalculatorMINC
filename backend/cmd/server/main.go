// Калькулятор затрат по Приказу Минцифры — точка входа.
// Роутинг — стандартный net/http.ServeMux (Go 1.22+, поддерживает методы и
// {параметры} пути "из коробки"), без сторонних веб-фреймворков.
package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/config"
	"cybercalc/internal/dbx"
	"cybercalc/internal/handlers"
	"cybercalc/internal/retention"
	appserver "cybercalc/internal/server"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		res, err := client.Get("http://127.0.0.1:8080/api/health")
		if err != nil {
			os.Exit(1)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}

	db, err := dbx.Connect(cfg.DSN())
	if err != nil {
		log.Fatalf("не удалось подключиться к БД: %v", err)
	}
	defer db.Close()

	migrationMode := len(os.Args) > 1 && os.Args[1] == "migrate"
	enrichmentMode := len(os.Args) > 1 && os.Args[1] == "enrich-directory"
	if migrationMode || os.Getenv("RUN_MIGRATIONS") != "false" {
		if err := dbx.RunMigrations(db, "/app/migrations"); err != nil {
			log.Fatalf("ошибка применения миграций: %v", err)
		}
	}

	if err := ensureBootstrapAdmin(db, cfg); err != nil {
		log.Fatalf("ошибка создания admin-пользователя по умолчанию: %v", err)
	}
	if migrationMode {
		if err := dbx.ProvisionRuntime(db, os.Getenv("RUNTIME_DB_USER"), os.Getenv("RUNTIME_DB_PASSWORD")); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "sync-it-companies" {
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, (32<<20)+1))
		if readErr != nil {
			log.Fatal(readErr)
		}
		count, importErr := handlers.ImportITCompanies(context.Background(), db, data, "")
		if importErr != nil {
			log.Fatal(importErr)
		}
		log.Printf("реестр ИТ-компаний: загружено %d записей", count)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "enrich-programs" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		query := ""
		if len(os.Args) > 2 {
			query = os.Args[2]
		}
		result, err := handlers.EnrichDirectoryProgramsForQuery(ctx, db, cfg.DirectoryEnrichLimit, query)
		if err != nil {
			log.Fatalf("направления: обработано %d: %v", result.Processed, err)
		}
		log.Printf("направления: обработано %d, получены коды %d, без данных %d", result.Processed, result.Matched, result.Unmatched)
		return
	}
	if enrichmentMode {
		limit, delayMS := 0, 500
		if raw := os.Getenv("DIRECTORY_ENRICH_LIMIT"); raw != "" {
			if value, parseErr := strconv.Atoi(raw); parseErr != nil || value < 0 {
				log.Fatal("DIRECTORY_ENRICH_LIMIT должен быть целым неотрицательным числом")
			} else {
				limit = value
			}
		}
		if raw := os.Getenv("DIRECTORY_ENRICH_DELAY_MS"); raw != "" {
			if value, parseErr := strconv.Atoi(raw); parseErr != nil || value < 0 || value > 60000 {
				log.Fatal("DIRECTORY_ENRICH_DELAY_MS должен быть числом от 0 до 60000")
			} else {
				delayMS = value
			}
		}
		result, enrichErr := handlers.EnrichEducationDirectory(context.Background(), db, limit, time.Duration(delayMS)*time.Millisecond)
		if enrichErr != nil {
			log.Fatalf("обогащение справочника остановлено после %d записей: %v", result.Processed, enrichErr)
		}
		log.Printf("обогащение завершено: обработано %d, найдено %d, без результата %d", result.Processed, result.Matched, result.Unmatched)
		return
	}
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		log.Fatalf("не удалось создать каталог загрузок: %v", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go retention.Run(db, 1*time.Hour, stop, cfg.UploadDir)
	go handlers.RunDirectorySync(db, cfg.DirectorySyncURL, time.Duration(cfg.DirectorySyncHours)*time.Hour, stop)
	if cfg.DirectoryEnrichOnStart {
		go handlers.RunDirectoryPrograms(db, cfg.DirectoryEnrichLimit, time.Duration(cfg.DirectorySyncHours)*time.Hour, stop)
		go func() {
			result, enrichErr := handlers.EnrichEducationDirectory(context.Background(), db, cfg.DirectoryEnrichLimit, time.Duration(cfg.DirectoryEnrichDelayMS)*time.Millisecond)
			if enrichErr != nil {
				log.Printf("автозаполнение ИНН/ОГРН остановлено после %d записей: %v", result.Processed, enrichErr)
				return
			}
			log.Printf("автозаполнение ИНН/ОГРН завершено: обработано %d, найдено %d, без результата %d", result.Processed, result.Matched, result.Unmatched)
		}()
	}

	mux := appserver.BuildRoutes(db, cfg)

	log.Printf("сервер запущен на %s", cfg.HTTPAddr)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		shutdown, stopShutdown := context.WithTimeout(context.Background(), 100*time.Second)
		defer stopShutdown()
		if err := server.Shutdown(shutdown); err != nil {
			log.Printf("shutdown: %v", err)
		}
		close(done)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server: %v", err)
		cancel()
	}
	<-done
}

func ensureBootstrapAdmin(db *sql.DB, cfg config.Config) error {
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		_, err := db.Exec(`UPDATE users SET entity_type='organization',updated_at=now()
			WHERE role='admin' AND entity_type IS NULL`)
		return err
	}
	hash, err := auth.HashPassword(cfg.AdminBootPassword)
	if err != nil {
		return err
	}
	result, err := db.Exec(
		`INSERT INTO users (email, password_hash, full_name, role, entity_type)
		 VALUES ($1,$2,$3,'admin','organization')
		 ON CONFLICT (email) DO NOTHING`,
		cfg.AdminBootEmail, hash, "Администратор",
	)
	if err != nil {
		return err
	}
	if created, rowsErr := result.RowsAffected(); rowsErr == nil && created == 1 {
		log.Printf("создан администратор по умолчанию: %s (смените пароль после первого входа!)", cfg.AdminBootEmail)
	}
	return nil
}
