// Калькулятор затрат по Приказу Минцифры — точка входа.
// Роутинг — стандартный net/http.ServeMux (Go 1.22+, поддерживает методы и
// {параметры} пути "из коробки"), без сторонних веб-фреймворков.
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
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

	db, err := dbx.Connect(cfg.DSN())
	if err != nil {
		log.Fatalf("не удалось подключиться к БД: %v", err)
	}
	defer db.Close()

	if err := dbx.RunMigrations(db, "/app/migrations"); err != nil {
		log.Fatalf("ошибка применения миграций: %v", err)
	}

	if err := ensureBootstrapAdmin(db, cfg); err != nil {
		log.Fatalf("ошибка создания admin-пользователя по умолчанию: %v", err)
	}
	if err := os.MkdirAll(cfg.UploadDir, 0o750); err != nil {
		log.Fatalf("не удалось создать каталог загрузок: %v", err)
	}

	stop := make(chan struct{})
	go retention.Run(db, 1*time.Hour, stop)

	mux := buildRoutes(db, cfg)

	log.Printf("сервер запущен на %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, mux); err != nil {
		log.Fatal(err)
	}
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

func buildRoutes(db *sql.DB, cfg config.Config) *http.ServeMux {
	mux := http.NewServeMux()

	authH := &handlers.AuthHandlers{DB: db}
	partnerH := &handlers.PartnerHandlers{DB: db}
	entryH := &handlers.EntryHandlers{DB: db, UploadDir: cfg.UploadDir}
	attachH := &handlers.AttachmentHandlers{DB: db, UploadDir: cfg.UploadDir}
	dashH := &handlers.DashboardHandlers{DB: db}
	adminH := &handlers.AdminHandlers{DB: db}
	reportH := &handlers.ReportHandlers{DB: db}

	// --- Аутентификация ---
	mux.HandleFunc("POST /api/auth/login", authH.Login)
	mux.HandleFunc("POST /api/auth/logout", authH.Logout)
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(db, authH.Me))
	mux.HandleFunc("POST /api/auth/entity-type", middleware.RequireAuth(db, authH.SetEntityType))

	// --- Партнёры ---
	mux.HandleFunc("GET /api/partners", middleware.RequireAuth(db, partnerH.List))
	mux.HandleFunc("POST /api/partners", middleware.RequireAuth(db, partnerH.Create))

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

	return mux
}
