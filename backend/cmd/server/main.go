// Калькулятор затрат по Приказу Минцифры — точка входа.
// Роутинг — стандартный net/http.ServeMux (Go 1.22+, поддерживает методы и
// {параметры} пути "из коробки"), без сторонних веб-фреймворков.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/config"
	"cybercalc/internal/dbx"
	"cybercalc/internal/handlers"
	"cybercalc/internal/middleware"
	"cybercalc/internal/retention"
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
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		log.Fatalf("не удалось создать каталог загрузок: %v", err)
	}

	stop := make(chan struct{})
	defer close(stop)
	go retention.Run(db, 1*time.Hour, stop, cfg.UploadDir)

	mux := buildRoutes(db, cfg)

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
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminBootPassword)
	if err != nil {
		return err
	}
	result, err := db.Exec(
		`INSERT INTO users (email, password_hash, full_name, role)
		 VALUES ($1,$2,$3,'admin')
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

func buildRoutes(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	authH := &handlers.AuthHandlers{DB: db, SessionTTL: time.Duration(cfg.SessionTTLh) * time.Hour, SecureCookie: cfg.CookieSecure, MFAKey: cfg.MFAKey, RequireMFA: cfg.Environment == "production"}
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db.PingContext(ctx) != nil {
			middleware.WriteError(w, 503, "база данных недоступна")
			return
		}
		middleware.WriteJSON(w, 200, map[string]string{"status": "ok"})
	})
	partnerH := &handlers.PartnerHandlers{DB: db}
	entryH := &handlers.EntryHandlers{DB: db}
	attachH := &handlers.AttachmentHandlers{DB: db, UploadDir: cfg.UploadDir, ScannerAddress: cfg.ScannerAddress, QuotaBytes: cfg.UploadQuotaBytes}
	dashH := &handlers.DashboardHandlers{DB: db}
	adminH := &handlers.AdminHandlers{DB: db}
	reportH := &handlers.ReportHandlers{DB: db}

	// --- Аутентификация ---
	mux.HandleFunc("POST /api/auth/login", authH.Login)
	mux.HandleFunc("POST /api/auth/logout", authH.Logout)
	mux.HandleFunc("POST /api/auth/password", middleware.RequireAuth(db, authH.ChangePassword))
	mux.HandleFunc("POST /api/auth/mfa/enroll", middleware.RequireAuth(db, authH.MFAEnroll))
	mux.HandleFunc("POST /api/auth/mfa/confirm", middleware.RequireAuth(db, authH.MFAConfirm))
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(db, authH.Me))
	mux.HandleFunc("POST /api/auth/entity-type", middleware.RequireAuth(db, authH.SetEntityType))

	// --- Партнёры ---
	mux.HandleFunc("GET /api/partners", middleware.RequireAuth(db, partnerH.List))
	mux.HandleFunc("POST /api/partners", middleware.RequireAuth(db, partnerH.Create))
	mux.HandleFunc("GET /api/directory", middleware.RequireAuth(db, partnerH.Directory))
	mux.HandleFunc("GET /api/mentors", middleware.RequireAuth(db, entryH.Mentors))
	mux.HandleFunc("POST /api/mentors", middleware.RequireAuth(db, entryH.CreateMentor))
	mux.HandleFunc("GET /api/obligations", middleware.RequireAuth(db, entryH.Obligations))
	mux.HandleFunc("GET /api/entries/import-template", middleware.RequireAuth(db, entryH.ImportTemplate))
	mux.HandleFunc("POST /api/entries/import", middleware.RequireAuth(db, entryH.Import))
	mux.HandleFunc("GET /api/admin/directory-template", middleware.RequireAdmin(db, partnerH.DirectoryTemplate))
	mux.HandleFunc("POST /api/admin/directory-import", middleware.RequireAdmin(db, partnerH.ImportDirectory))

	// --- Категории активностей (справочник с полями формы) ---
	mux.HandleFunc("GET /api/categories", middleware.RequireAuth(db, entryH.Categories))

	// --- Записи плана/факта ---
	mux.HandleFunc("GET /api/entries", middleware.RequireAuth(db, entryH.List))
	mux.HandleFunc("POST /api/entries", middleware.RequireAuth(db, entryH.Create))
	mux.HandleFunc("PUT /api/entries/{id}", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		entryH.Update(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/comments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		entryH.Comments(w, r, u, r.PathValue("id"))
	}))

	// --- Вложения (подтверждающие документы факта) ---
	mux.HandleFunc("POST /api/entries/{id}/attachments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.Upload(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/entries/{id}/attachments", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.ListForEntry(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/attachments/{id}/download", middleware.RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		attachH.Download(w, r, u, r.PathValue("id"))
	}))

	// --- Дашборд ---
	mux.HandleFunc("GET /api/dashboard", middleware.RequireAuth(db, dashH.Get))
	mux.HandleFunc("POST /api/dashboard/target", middleware.RequireAuth(db, dashH.SetBudgetTarget))

	// --- Отчёты xlsx ---
	mux.HandleFunc("GET /api/reports/export", middleware.RequireAuth(db, reportH.Export))

	// --- Админка ---
	mux.HandleFunc("GET /api/admin/users", middleware.RequireAdmin(db, adminH.ListUsers))
	mux.HandleFunc("POST /api/admin/users", middleware.RequireAdmin(db, adminH.CreateUser))
	mux.HandleFunc("PATCH /api/admin/users/{id}", middleware.RequireAdmin(db, func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
		adminH.UpdateUser(w, r, u, r.PathValue("id"))
	}))
	mux.HandleFunc("GET /api/admin/settings", middleware.RequireAdmin(db, adminH.GetSettings))
	mux.HandleFunc("POST /api/admin/settings", middleware.RequireAdmin(db, adminH.UpdateSetting))
	mux.HandleFunc("GET /api/admin/logs", middleware.RequireAdmin(db, adminH.AuditLog))

	return middleware.Security(mux, cfg.PublicURL, cfg.Environment == "production")
}
