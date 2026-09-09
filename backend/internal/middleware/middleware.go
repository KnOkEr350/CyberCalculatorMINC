// Package middleware содержит сквозную логику HTTP-слоя: аутентификацию,
// проверку ролей и общие помощники для JSON-ответов. Роутинг выполняется
// стандартным net/http.ServeMux (Go 1.22+, с поддержкой методов и
// параметров пути) — сторонний роутер не используется.
package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"cybercalc/internal/auth"
	"cybercalc/internal/models"
)

type ctxKey string

const (
	ctxUserID ctxKey = "user_id"
	ctxRole   ctxKey = "role"
	ctxEntity ctxKey = "entity_type"
)

// AuthUser хранит информацию о текущем пользователе в контексте запроса.
type AuthUser struct {
	ID         string
	Role       models.Role
	EntityType models.EntityType
	PartnerID  *string
}

func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("json encode error: %v", err)
		}
	}
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// RequireAuth проверяет сессию и кладёт пользователя в контекст.
func RequireAuth(db *sql.DB, next func(http.ResponseWriter, *http.Request, AuthUser)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserIDFromRequest(r, db)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "требуется авторизация")
			return
		}
		var u AuthUser
		var entityType sql.NullString
		var partnerID sql.NullString
		var role string
		var mfaEnabled bool
		err := db.QueryRowContext(r.Context(), `SELECT id, role, entity_type, partner_id,mfa_secret IS NOT NULL FROM users WHERE id = $1 AND is_active`, userID).
			Scan(&u.ID, &role, &entityType, &partnerID, &mfaEnabled)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "пользователь не найден или деактивирован")
			return
		}
		u.Role = models.Role(role)
		if required, _ := r.Context().Value(ctxKey("require_mfa")).(bool); required && role == "admin" && !mfaEnabled {
			switch r.URL.Path {
			case "/api/auth/me", "/api/auth/password", "/api/auth/mfa/enroll", "/api/auth/mfa/confirm":
			default:
				WriteError(w, 403, "сначала настройте двухфакторную защиту администратора")
				return
			}
		}
		if entityType.Valid {
			u.EntityType = models.EntityType(entityType.String)
		}
		if partnerID.Valid {
			pid := partnerID.String
			u.PartnerID = &pid
		}
		next(w, r, u)
	}
}

// RequireAdmin — то же самое, но только для роли admin.
func RequireAdmin(db *sql.DB, next func(http.ResponseWriter, *http.Request, AuthUser)) http.HandlerFunc {
	return RequireAuth(db, func(w http.ResponseWriter, r *http.Request, u AuthUser) {
		if u.Role != models.RoleAdmin {
			WriteError(w, http.StatusForbidden, "требуются права администратора")
			return
		}
		next(w, r, u)
	})
}

func WithContext(r *http.Request, u AuthUser) *http.Request {
	ctx := context.WithValue(r.Context(), ctxUserID, u.ID)
	return r.WithContext(ctx)
}
