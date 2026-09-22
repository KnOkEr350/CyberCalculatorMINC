package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"cybercalc/internal/auth"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type AdminHandlers struct {
	DB *sql.DB
}

// --- Пользователи (в т.ч. создание дополнительных админов) -----------------

type createUserRequest struct {
	Email       string  `json:"email"`
	Password    string  `json:"password"`
	FullName    string  `json:"full_name"`
	Role        string  `json:"role"` // admin|moderator|user
	EntityType  string  `json:"entity_type,omitempty"`
	PartnerID   *string `json:"partner_id,omitempty"`
	ITCompanyID *string `json:"it_company_id,omitempty"`
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
	if req.EntityType == "" {
		middleware.WriteError(w, 400, "назначьте тип профиля")
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
	if req.ITCompanyID != nil {
		var exists bool
		if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM accredited_it_companies WHERE id::text=$1 AND accreditation_status='active')`, *req.ITCompanyID).Scan(&exists); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить выбранную ИТ-компанию")
			return
		}
		if !exists {
			middleware.WriteError(w, http.StatusBadRequest, "выбранная ИТ-компания не найдена или не аккредитована")
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
		`INSERT INTO users (email, password_hash, full_name, role, entity_type, partner_id, it_company_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		req.Email, hash, req.FullName, req.Role, entityType, req.PartnerID, req.ITCompanyID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось создать пользователя; возможно, email уже занят")
		return
	}
	if req.EntityType == string(models.EntityOrganization) && req.Role == string(models.RoleCurator) && req.PartnerID != nil {
		var belongs bool
		if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text=$1 AND it_company_id::text=$2)`, *req.PartnerID, *req.ITCompanyID).Scan(&belongs); err != nil || !belongs {
			middleware.WriteError(w, 400, "закреплённая ОО должна относиться к выбранной ИТ-компании")
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO user_partner_assignments(user_id,partner_id,assigned_by) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, *req.PartnerID, admin.ID); err != nil {
			middleware.WriteError(w, 500, "не удалось закрепить ОО за куратором")
			return
		}
	}
	if logAudit(r.Context(), tx, "user", id, "create", admin.ID, "", nil, map[string]string{"email": req.Email, "role": req.Role}) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// ITCompanyOptions supplies only the fields needed to assign a user to a company.
// Access to the full accreditation directory still depends on the profile type.
func (h *AdminHandlers) ITCompanyOptions(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,name,inn FROM accredited_it_companies
		WHERE accreditation_status='active' ORDER BY name,id`+page)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось загрузить ИТ-компании для назначения")
		return
	}
	defer rows.Close()
	out := make([]map[string]string, 0)
	for rows.Next() {
		var id, name, inn string
		if err := rows.Scan(&id, &name, &inn); err != nil {
			middleware.WriteError(w, 500, "не удалось прочитать ИТ-компании для назначения")
			return
		}
		out = append(out, map[string]string{"id": id, "name": name, "inn": inn})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "не удалось прочитать ИТ-компании для назначения")
		return
	}
	writePage(w, r, out)
}

func (h *AdminHandlers) ListUsers(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	q := strings.Join(strings.Fields(r.URL.Query().Get("q")), " ")
	if utf8.RuneCountInString(q) > 200 {
		middleware.WriteError(w, http.StatusBadRequest, "поисковый запрос не должен превышать 200 символов")
		return
	}
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, email, full_name, role, entity_type, partner_id, it_company_id, is_active, created_at
		FROM users
		WHERE ($1='' OR POSITION(lower($1) IN lower(email)) > 0 OR POSITION(lower($1) IN lower(full_name)) > 0)
		ORDER BY created_at,id`+page, q)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	out := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		var entityType, partnerID, itCompanyID sql.NullString
		if err := rows.Scan(&u.ID, &u.Email, &u.FullName, &u.Role, &entityType, &partnerID, &itCompanyID, &u.IsActive, &u.CreatedAt); err != nil {
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
		if itCompanyID.Valid {
			id := itCompanyID.String
			u.ITCompanyID = &id
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
	IsActive    *bool   `json:"is_active,omitempty"`
	Role        *string `json:"role,omitempty"`
	EntityType  *string `json:"entity_type,omitempty"`
	PartnerID   *string `json:"partner_id,omitempty"`
	ITCompanyID *string `json:"it_company_id,omitempty"`
}

func (h *AdminHandlers) UpdateUser(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser, userID string) {
	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.Role != nil {
		if !models.ValidRole(models.Role(*req.Role)) {
			middleware.WriteError(w, http.StatusBadRequest, "неизвестная роль RBAC")
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
	var role, entity, partner, itCompany string
	if tx.QueryRowContext(r.Context(), `SELECT is_active,role,COALESCE(entity_type,''),COALESCE(partner_id::text,''),COALESCE(it_company_id::text,'') FROM users WHERE id::text=$1 FOR UPDATE`, userID).Scan(&active, &role, &entity, &partner, &itCompany) != nil {
		middleware.WriteError(w, 404, "пользователь не найден")
		return
	}
	old := map[string]interface{}{"is_active": active, "role": role, "entity_type": entity, "partner_id": partner, "it_company_id": itCompany}
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
	if req.ITCompanyID != nil {
		itCompany = *req.ITCompanyID
	}
	if entity == "edu_institution" {
		itCompany = ""
	} else if role != string(models.RoleCurator) {
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
	if role != string(models.RoleSuperAdmin) && entity == "organization" && itCompany == "" {
		middleware.WriteError(w, 400, "назначьте ИТ-компанию")
		return
	}
	if itCompany != "" {
		var exists bool
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM accredited_it_companies WHERE id::text=$1 AND accreditation_status='active')`, itCompany).Scan(&exists) != nil || !exists {
			middleware.WriteError(w, 400, "ИТ-компания не найдена или не аккредитована")
			return
		}
	}
	if entity == "organization" && role == string(models.RoleCurator) && partner != "" {
		var belongs bool
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text=$1 AND it_company_id::text=$2)`, partner, itCompany).Scan(&belongs) != nil || !belongs {
			middleware.WriteError(w, 400, "закреплённая ОО должна относиться к выбранной ИТ-компании")
			return
		}
	}
	if !active || role != string(models.RoleSuperAdmin) {
		var count int
		if tx.QueryRowContext(r.Context(), `SELECT count(*) FROM users WHERE role='super_admin' AND is_active AND id::text<>$1`, userID).Scan(&count) != nil {
			middleware.WriteError(w, 500, "ошибка проверки администраторов")
			return
		}
		if count == 0 {
			middleware.WriteError(w, 400, "нельзя отключить последнего администратора")
			return
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE users SET is_active=$1,role=$2,entity_type=NULLIF($3,''),partner_id=NULLIF($4,'')::uuid,it_company_id=NULLIF($5,'')::uuid,updated_at=now() WHERE id::text=$6`, active, role, entity, partner, itCompany, userID); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM user_partner_assignments WHERE user_id::text=$1`, userID); err != nil {
		middleware.WriteError(w, 500, "ошибка обновления закреплённых ОО")
		return
	}
	if entity == "organization" && role == string(models.RoleCurator) && partner != "" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO user_partner_assignments(user_id,partner_id,assigned_by) VALUES($1,$2,$3)`, userID, partner, admin.ID); err != nil {
			middleware.WriteError(w, 500, "ошибка закрепления ОО")
			return
		}
	}
	if logAudit(r.Context(), tx, "user", userID, "update", admin.ID, "", old, req) != nil {
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
	if logAudit(r.Context(), tx, "settings", "", "settings_change", admin.ID, "", nil, req) != nil || tx.Commit() != nil {
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
		conds = append(conds, "a.entity_type = "+arg(v))
	}
	if v := q.Get("user_id"); v != "" {
		conds = append(conds, "a.user_id = "+arg(v))
	}
	if v := q.Get("request_id"); v != "" {
		conds = append(conds, "a.request_id = "+arg(v))
	}
	limit := 200
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	// SQL structure comes only from fixed fragments; values remain positional parameters.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT a.id, a.actor_type, a.entity_type, a.entity_id, a.action, a.user_id,
		COALESCE(u.email,''),COALESCE(u.full_name,''),a.comment_text,a.old_value,a.new_value,a.request_id,a.created_at
		FROM audit_log a LEFT JOIN users u ON u.id=a.user_id WHERE ` + joinAnd(conds) +
		` ORDER BY a.created_at DESC LIMIT ` + strconv.Itoa(limit)
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
		var userEmail, userName string
		var oldRaw, newRaw []byte
		if err := rows.Scan(&item.ID, &item.Actor.Type, &item.EntityType, &entityID, &item.Action, &userID,
			&userEmail, &userName, &comment, &oldRaw, &newRaw, &item.RequestID, &item.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		if entityID.Valid {
			v := entityID.String
			item.EntityID = &v
		}
		item.Entity = models.AuditEntity{Type: item.EntityType, ID: item.EntityID}
		if userID.Valid {
			v := userID.String
			item.UserID = &v
			item.Actor.ID = &v
		}
		if userEmail != "" {
			item.UserEmail = &userEmail
			item.Actor.Email = &userEmail
		}
		if userName != "" {
			item.UserName = &userName
			item.Actor.Name = &userName
		}
		if comment.Valid {
			v := comment.String
			item.CommentText = &v
		}
		if len(oldRaw) > 0 {
			json.Unmarshal(oldRaw, &item.OldValue)
			item.Old = item.OldValue
		}
		if len(newRaw) > 0 {
			json.Unmarshal(newRaw, &item.NewValue)
			item.New = item.NewValue
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения аудита")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}
