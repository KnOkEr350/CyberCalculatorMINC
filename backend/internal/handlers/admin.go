package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type AdminHandlers struct {
	DB *sql.DB
}

// --- Пользователи (в т.ч. создание дополнительных админов) -----------------

type createUserRequest struct {
	Email      string  `json:"email"`
	Password   string  `json:"password"`
	FullName   string  `json:"full_name"`
	Role       string  `json:"role"` // admin|user
	EntityType string  `json:"entity_type,omitempty"`
	PartnerID  *string `json:"partner_id,omitempty"`
}

func (h *AdminHandlers) CreateUser(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if err := validateAndNormalizeNewUser(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Role == "admin" {
		req.EntityType = "organization"
		req.PartnerID = nil
	}
	if req.EntityType == "" || (req.EntityType == "edu_institution" && req.PartnerID == nil) {
		middleware.WriteError(w, 400, "назначьте тип профиля и учебное заведение для представителя ОО")
		return
	}
	if req.PartnerID != nil {
		var exists bool
		if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text = $1)`, *req.PartnerID).Scan(&exists); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить выбранного партнёра")
			return
		}
		if !exists {
			middleware.WriteError(w, http.StatusBadRequest, "выбранный партнёр не найден")
			return
		}
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка хеширования пароля")
		return
	}

	var entityType interface{}
	if req.EntityType != "" {
		entityType = req.EntityType
	}
	var id string
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO users (email, password_hash, full_name, role, entity_type, partner_id)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		req.Email, hash, req.FullName, req.Role, entityType, req.PartnerID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось создать пользователя; возможно, email уже занят")
		return
	}
	if logAudit(tx, "user", id, "create", admin.ID, "", nil, map[string]string{"email": req.Email, "role": req.Role}) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (h *AdminHandlers) ListUsers(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, email, full_name, role, entity_type, partner_id, is_active, created_at
		FROM users ORDER BY created_at,id`+page)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	out := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		var entityType, partnerID sql.NullString
		if err := rows.Scan(&u.ID, &u.Email, &u.FullName, &u.Role, &entityType, &partnerID, &u.IsActive, &u.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		if entityType.Valid {
			u.EntityType = models.EntityType(entityType.String)
		}
		if partnerID.Valid {
			p := partnerID.String
			u.PartnerID = &p
		}
		out = append(out, u)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения пользователей")
		return
	}
	writePage(w, r, out)
}

type updateUserRequest struct {
	IsActive   *bool   `json:"is_active,omitempty"`
	Role       *string `json:"role,omitempty"`
	EntityType *string `json:"entity_type,omitempty"`
	PartnerID  *string `json:"partner_id,omitempty"`
}

func (h *AdminHandlers) UpdateUser(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser, userID string) {
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.Role != nil {
		if *req.Role != string(models.RoleAdmin) && *req.Role != string(models.RoleUser) {
			middleware.WriteError(w, http.StatusBadRequest, "role должен быть admin или user")
			return
		}
	}
	if req.EntityType != nil && *req.EntityType != "organization" && *req.EntityType != "edu_institution" {
		middleware.WriteError(w, 400, "некорректный тип профиля")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(270004)`); err != nil {
		middleware.WriteError(w, 500, "ошибка блокировки")
		return
	}
	var active bool
	var role, entity, partner string
	if tx.QueryRowContext(r.Context(), `SELECT is_active,role,COALESCE(entity_type,''),COALESCE(partner_id::text,'') FROM users WHERE id::text=$1 FOR UPDATE`, userID).Scan(&active, &role, &entity, &partner) != nil {
		middleware.WriteError(w, 404, "пользователь не найден")
		return
	}
	old := map[string]interface{}{"is_active": active, "role": role, "entity_type": entity, "partner_id": partner}
	if req.IsActive != nil {
		active = *req.IsActive
	}
	if req.Role != nil {
		role = *req.Role
	}
	if req.EntityType != nil {
		entity = *req.EntityType
	}
	if req.PartnerID != nil {
		partner = *req.PartnerID
	}
	if role == "admin" {
		entity = "organization"
	}
	if entity != "edu_institution" {
		partner = ""
	}
	if entity == "edu_institution" && partner == "" {
		middleware.WriteError(w, 400, "назначьте учебное заведение")
		return
	}
	if partner != "" {
		var exists bool
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text=$1)`, partner).Scan(&exists) != nil || !exists {
			middleware.WriteError(w, 400, "партнёр не найден")
			return
		}
	}
	if !active || role != "admin" {
		var count int
		if tx.QueryRowContext(r.Context(), `SELECT count(*) FROM users WHERE role='admin' AND is_active AND id::text<>$1`, userID).Scan(&count) != nil {
			middleware.WriteError(w, 500, "ошибка проверки администраторов")
			return
		}
		if count == 0 {
			middleware.WriteError(w, 400, "нельзя отключить последнего администратора")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET is_active=$1,role=$2,entity_type=NULLIF($3,''),partner_id=NULLIF($4,'')::uuid,updated_at=now() WHERE id::text=$5`, active, role, entity, partner, userID); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	if logAudit(tx, "user", userID, "update", admin.ID, "", old, req) != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id::text=$1`, userID); err != nil {
		middleware.WriteError(w, 500, "ошибка отзыва сессий")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Настройки (сроки хранения и т.п.) -------------------------------------

func (h *AdminHandlers) GetSettings(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT key, value FROM settings`)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			middleware.WriteError(w, 500, "ошибка чтения настроек")
			return
		}
		out[k] = v
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения настроек")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

type updateSettingRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// UpdateSetting позволяет админу регулировать, например,
// attachment_retention_days ("но администратор может это регулировать" — ТЗ).
func (h *AdminHandlers) UpdateSetting(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	var req updateSettingRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.Key == "" {
		middleware.WriteError(w, http.StatusBadRequest, "key обязателен")
		return
	}
	if req.Key != "audit_log_retention_days" && req.Key != "attachment_retention_days" {
		middleware.WriteError(w, http.StatusBadRequest, "неизвестная настройка")
		return
	}
	if req.Key == "audit_log_retention_days" || req.Key == "attachment_retention_days" {
		if v, err := strconv.Atoi(req.Value); err != nil || v <= 0 || v > 3650 {
			middleware.WriteError(w, http.StatusBadRequest, "срок хранения должен быть целым числом от 1 до 3650 дней")
			return
		}
	}
	if req.Key == "audit_log_retention_days" {
		days, _ := strconv.Atoi(req.Value)
		if days < 60 {
			middleware.WriteError(w, 400, "журнал аудита хранится минимум 60 дней")
			return
		}
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(),
		`INSERT INTO settings (key, value, updated_by) VALUES ($1,$2,$3)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_by = $3, updated_at = now()`,
		req.Key, req.Value, admin.ID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	// audit_log.entity_id имеет тип UUID, а ключ настройки — строка; сам ключ
	// сохраняется в new_value, поэтому UUID для этого типа события не задаём.
	if logAudit(tx, "settings", "", "settings_change", admin.ID, "", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Журнал изменений (виден только админам, хранится N дней) --------------

func (h *AdminHandlers) AuditLog(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	q := r.URL.Query()
	conds := []string{"1=1"}
	args := []interface{}{}
	arg := func(v interface{}) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if v := q.Get("entity_type"); v != "" {
		conds = append(conds, "entity_type = "+arg(v))
	}
	if v := q.Get("user_id"); v != "" {
		conds = append(conds, "user_id = "+arg(v))
	}
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	query := `SELECT id, entity_type, entity_id, action, user_id, comment_text, old_value, new_value, created_at
		FROM audit_log WHERE ` + joinAnd(conds) + ` ORDER BY created_at DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	out := make([]models.AuditLogItem, 0)
	for rows.Next() {
		var item models.AuditLogItem
		var entityID, userID, comment sql.NullString
		var oldRaw, newRaw []byte
		if err := rows.Scan(&item.ID, &item.EntityType, &entityID, &item.Action, &userID, &comment, &oldRaw, &newRaw, &item.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		if entityID.Valid {
			v := entityID.String
			item.EntityID = &v
		}
		if userID.Valid {
			v := userID.String
			item.UserID = &v
		}
		if comment.Valid {
			v := comment.String
			item.CommentText = &v
		}
		if len(oldRaw) > 0 {
			json.Unmarshal(oldRaw, &item.OldValue)
		}
		if len(newRaw) > 0 {
			json.Unmarshal(newRaw, &item.NewValue)
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения аудита")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}
