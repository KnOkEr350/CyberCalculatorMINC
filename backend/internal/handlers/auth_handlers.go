package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type AuthHandlers struct {
	DB *sql.DB
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}

	var id, passwordHash string
	var isActive bool
	err := h.DB.QueryRow(`SELECT id, password_hash, is_active FROM users WHERE email = $1`, strings.ToLower(strings.TrimSpace(req.Email))).
		Scan(&id, &passwordHash, &isActive)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusUnauthorized, "неверный email или пароль")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сервера")
		return
	}
	if !isActive {
		middleware.WriteError(w, http.StatusForbidden, "учётная запись отключена")
		return
	}

	ok, err := auth.VerifyPassword(req.Password, passwordHash)
	if err != nil || !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "неверный email или пароль")
		return
	}

	if err := auth.CreateSession(w, h.DB, id, 12*time.Hour); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось создать сессию")
		return
	}
	logAudit(h.DB, "user", id, "login", id, "", nil, nil)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	auth.DestroySession(w, r, h.DB)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var user models.User
	var entityType, partnerID sql.NullString
	err := h.DB.QueryRow(`SELECT id, email, full_name, role, entity_type, partner_id, is_active, created_at
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
	middleware.WriteJSON(w, http.StatusOK, user)
}

// SetEntityType — реализация пункта ТЗ "администратор должен выбирать, кто
// он (организация или вуз)". Доступно любому авторизованному пользователю
// для собственного профиля.
type setEntityTypeRequest struct {
	EntityType string  `json:"entity_type"`
	PartnerID  *string `json:"partner_id"`
}

func (h *AuthHandlers) SetEntityType(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req setEntityTypeRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.EntityType != string(models.EntityOrganization) && req.EntityType != string(models.EntityEduInst) {
		middleware.WriteError(w, http.StatusBadRequest, "entity_type должен быть organization или edu_institution")
		return
	}
	if req.EntityType == string(models.EntityOrganization) {
		req.PartnerID = nil
	} else {
		if req.PartnerID == nil || strings.TrimSpace(*req.PartnerID) == "" {
			middleware.WriteError(w, http.StatusBadRequest, "для образовательной организации необходимо выбрать партнёра")
			return
		}
		var exists bool
		if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM partners WHERE id::text = $1)`, strings.TrimSpace(*req.PartnerID)).Scan(&exists); err != nil || !exists {
			middleware.WriteError(w, http.StatusBadRequest, "выбранный партнёр не найден")
			return
		}
	}
	_, err := h.DB.Exec(`UPDATE users SET entity_type = $1, partner_id = $2, updated_at = now() WHERE id = $3`,
		req.EntityType, req.PartnerID, u.ID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	logAudit(h.DB, "user", u.ID, "update", u.ID, "выбор роли: организация/вуз", nil, req)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
