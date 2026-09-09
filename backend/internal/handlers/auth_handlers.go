package handlers

import (
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type AuthHandlers struct {
	DB           *sql.DB
	SessionTTL   time.Duration
	SecureCookie bool
	MFAKey       string
	RequireMFA   bool
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code,omitempty"`
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if len(email) > 254 || len(req.Password) > 512 {
		middleware.WriteError(w, 401, "неверный email или пароль")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	for key, limit := range map[string]int{"email:" + email: 12, "peer:" + ip: 100} {
		allowed, err := auth.AllowAttempt(r.Context(), h.DB, key, limit)
		if err != nil {
			middleware.WriteError(w, 503, "вход временно недоступен")
			return
		}
		if !allowed {
			w.Header().Set("Retry-After", "900")
			middleware.WriteError(w, 429, "слишком много попыток; повторите через 15 минут")
			return
		}
	}

	var id, passwordHash string
	var isActive bool
	var mfa sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `SELECT id, password_hash, is_active,mfa_secret FROM users WHERE email = $1`, strings.ToLower(strings.TrimSpace(req.Email))).
		Scan(&id, &passwordHash, &isActive, &mfa)
	if err == sql.ErrNoRows {
		_, _ = auth.VerifyPassword(req.Password, dummyPasswordHash)
		slog.Warn("login failed", "request_id", w.Header().Get("X-Request-ID"))
		middleware.WriteError(w, http.StatusUnauthorized, "неверный email или пароль")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	ok, err := auth.VerifyPassword(req.Password, passwordHash)
	if err != nil || !ok || !isActive {
		slog.Warn("login failed", "request_id", w.Header().Get("X-Request-ID"))
		middleware.WriteError(w, http.StatusUnauthorized, "неверный email или пароль")
		return
	}

	if auth.NeedsRehash(passwordHash) {
		hash, hashErr := auth.HashPassword(req.Password)
		if hashErr != nil {
			middleware.WriteError(w, 500, "ошибка сервера")
			return
		}
		if _, err := h.DB.ExecContext(r.Context(), `UPDATE users SET password_hash=$1 WHERE id=$2 AND password_hash=$3`, hash, id, passwordHash); err != nil {
			middleware.WriteError(w, 500, "ошибка сервера")
			return
		}
	}
	if mfa.Valid && !h.verifySecondFactor(r, id, mfa.String, req.Code) {
		middleware.WriteError(w, 401, "введите действующий код приложения-аутентификатора или резервный код")
		return
	}
	ttl := h.SessionTTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	if err := logAudit(h.DB, "user", id, "login", id, "", nil, nil); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := auth.CreateSession(w, h.DB, id, ttl, h.SecureCookie); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось создать сессию")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// A fixed valid-cost hash prevents skipping the KDF for unknown accounts.
const dummyPasswordHash = "pbkdf2$600000$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func (h *AuthHandlers) ChangePassword(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	allowed, err := auth.AllowAttempt(r.Context(), h.DB, "password:"+u.ID, 8)
	if err != nil || !allowed {
		middleware.WriteError(w, 429, "повторите позже")
		return
	}
	check := createUserRequest{Email: "check@example.com", FullName: "Проверка пароля", Password: req.New, Role: "user"}
	if err := validateAndNormalizeNewUser(&check); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	var old string
	if tx.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&old) != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if ok, _ := auth.VerifyPassword(req.Current, old); !ok {
		middleware.WriteError(w, 400, "неверный текущий пароль")
		return
	}
	if req.Current == req.New {
		middleware.WriteError(w, 400, "новый пароль должен отличаться")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET password_hash=$1, updated_at=now() WHERE id=$2`, hash, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=$1`, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	if logAudit(tx, "user", u.ID, "password_change", u.ID, "все сессии отозваны", nil, nil) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	auth.DestroySession(w, r, h.DB)
	middleware.WriteJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	auth.DestroySession(w, r, h.DB)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var user models.User
	var entityType, partnerID sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `SELECT id, email, full_name, role, entity_type, partner_id, is_active, created_at
		FROM users WHERE id = $1`, u.ID).
		Scan(&user.ID, &user.Email, &user.FullName, &user.Role, &entityType, &partnerID, &user.IsActive, &user.CreatedAt)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if entityType.Valid {
		user.EntityType = models.EntityType(entityType.String)
	}
	if partnerID.Valid {
		p := partnerID.String
		user.PartnerID = &p
	}
	if err := h.DB.QueryRowContext(r.Context(), `SELECT mfa_secret IS NOT NULL FROM users WHERE id=$1`, u.ID).Scan(&user.MFAEnabled); err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	user.MFARequired = h.RequireMFA && user.Role == models.RoleAdmin && !user.MFAEnabled
	user.MFAAvailable = h.MFAKey != ""
	middleware.WriteJSON(w, http.StatusOK, user)
}

// Legacy setup endpoint. Only an admin may initialize the Cyberprotect profile;
// partner/staff assignments for other users belong to AdminHandlers.UpdateUser.
type setEntityTypeRequest struct {
	EntityType string  `json:"entity_type"`
	PartnerID  *string `json:"partner_id"`
}

func (h *AuthHandlers) SetEntityType(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if u.Role != models.RoleAdmin {
		middleware.WriteError(w, 403, "тип профиля и партнёра назначает администратор")
		return
	}
	var req setEntityTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.EntityType != string(models.EntityOrganization) || req.PartnerID != nil {
		middleware.WriteError(w, http.StatusBadRequest, "администратор представляет Киберпротект; профили представителей ОО назначаются в админке")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE users SET entity_type = $1, partner_id = $2, updated_at = now() WHERE id = $3`,
		req.EntityType, req.PartnerID, u.ID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	if logAudit(tx, "user", u.ID, "update", u.ID, "выбор роли: организация/вуз", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
