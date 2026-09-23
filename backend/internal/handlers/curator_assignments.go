package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
)

// CuratorAssignmentHandlers — закрепление кураторов за партнёрами (DATA-09).
type CuratorAssignmentHandlers struct{ DB *sql.DB }

type curatorAssignmentRequest struct {
	CuratorID  string `json:"curator_id"`
	PartnerID  string `json:"partner_id"`
	ValidFrom  string `json:"valid_from"`
	ValidUntil string `json:"valid_until"`
	Reason     string `json:"reason"`
}

type curatorAssignmentEndRequest struct {
	ValidUntil string `json:"valid_until"`
}

type curatorAssignmentRevokeRequest struct {
	Reason string `json:"reason"`
}

func canManageCuratorAssignments(u middleware.AuthUser) bool {
	return u.EntityType == models.EntityOrganization && rbac.Allows(u.Role, rbac.ManageCuratorAssignments)
}

// assignmentTenant — арендатор, которым ограничен управляющий; пусто у
// системного администратора без арендатора.
func assignmentTenant(u middleware.AuthUser) string {
	if u.ITCompanyID != nil {
		return *u.ITCompanyID
	}
	return ""
}

func parseDay(value string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(value))
}

func writeAssignmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, curators.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, curators.ErrOverlap), errors.Is(err, curators.ErrAlreadyRevoked), errors.Is(err, curators.ErrNothingToChange):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, curators.ErrNotCurator), errors.Is(err, curators.ErrForeignTenant),
		errors.Is(err, curators.ErrInvalidPeriod), errors.Is(err, curators.ErrReasonRequired):
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка закрепления куратора")
	}
}

// List отдаёт закрепления. Управляющий видит закрепления своего арендатора,
// куратор — только свои.
func (h *CuratorAssignmentHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	filter := curators.Filter{
		CuratorID: strings.TrimSpace(r.URL.Query().Get("curator_id")), PartnerID: strings.TrimSpace(r.URL.Query().Get("partner_id")),
		ActiveOnly: r.URL.Query().Get("active") == "1" || r.URL.Query().Get("active") == "true",
	}
	switch {
	case canManageCuratorAssignments(u):
		filter.ITCompanyID = assignmentTenant(u)
	case u.EntityType == models.EntityOrganization && u.Role == models.RoleCurator:
		filter.CuratorID = u.ID
	default:
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к закреплениям кураторов")
		return
	}
	list, err := curators.List(r.Context(), h.DB, filter, time.Now())
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения закреплений")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, list)
}

// Create закрепляет куратора за партнёром на период.
func (h *CuratorAssignmentHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, http.StatusForbidden, "закреплять кураторов может администрация")
		return
	}
	var req curatorAssignmentRequest
	if err := decodeJSON(r, &req); err != nil || req.CuratorID == "" || req.PartnerID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите куратора и партнёра")
		return
	}
	now := time.Now()
	from := curators.Today(now)
	if strings.TrimSpace(req.ValidFrom) != "" {
		parsed, err := parseDay(req.ValidFrom)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "valid_from: ожидается дата ГГГГ-ММ-ДД")
			return
		}
		from = parsed
	}
	var until *time.Time
	if strings.TrimSpace(req.ValidUntil) != "" {
		parsed, err := parseDay(req.ValidUntil)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "valid_until: ожидается дата ГГГГ-ММ-ДД")
			return
		}
		until = &parsed
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	// Граница арендатора: управляющий не закрепляет чужих кураторов и не
	// закрепляет за чужими партнёрами. Ответ одинаков для «нет» и «чужой».
	if tenant := assignmentTenant(u); tenant != "" {
		var own bool
		if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id::text=$1 AND it_company_id::text=$3)
			AND EXISTS(SELECT 1 FROM partners WHERE id::text=$2 AND it_company_id::text=$3)`, req.CuratorID, req.PartnerID, tenant).Scan(&own); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка проверки арендатора")
			return
		}
		if !own {
			middleware.WriteError(w, http.StatusNotFound, "куратор или партнёр не найден")
			return
		}
	}
	assignment, err := curators.Assign(r.Context(), tx, curators.NewInput{CuratorID: req.CuratorID, PartnerID: req.PartnerID,
		From: from, Until: until, AssignedBy: u.ID, Reason: req.Reason}, now)
	if err != nil {
		writeAssignmentError(w, err)
		return
	}
	if logAudit(r.Context(), tx, "curator_assignment", assignment.ID, "assign", u.ID, strings.TrimSpace(req.Reason), nil, assignment) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения закрепления")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, assignment)
}

func (h *CuratorAssignmentHandlers) load(w http.ResponseWriter, r *http.Request, tx *sql.Tx, u middleware.AuthUser, id string) (curators.Assignment, bool) {
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, http.StatusForbidden, "изменять закрепления кураторов может администрация")
		return curators.Assignment{}, false
	}
	current, err := curators.Get(r.Context(), tx, id, time.Now())
	if err != nil {
		writeAssignmentError(w, err)
		return current, false
	}
	if tenant := assignmentTenant(u); tenant != "" && current.ITCompanyID != tenant {
		middleware.WriteError(w, http.StatusNotFound, curators.ErrNotFound.Error())
		return current, false
	}
	return current, true
}

// End сокращает период закрепления: куратор работает по указанный день включительно.
func (h *CuratorAssignmentHandlers) End(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	// Полномочие проверяется до разбора тела: роль без права не должна получать
	// подсказок о том, что не так с её запросом.
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, http.StatusForbidden, "изменять закрепления кураторов может администрация")
		return
	}
	var req curatorAssignmentEndRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	until, err := parseDay(req.ValidUntil)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "valid_until: ожидается дата ГГГГ-ММ-ДД")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	before, ok := h.load(w, r, tx, u, id)
	if !ok {
		return
	}
	after, err := curators.End(r.Context(), tx, id, until, time.Now())
	if err != nil {
		writeAssignmentError(w, err)
		return
	}
	if logAudit(r.Context(), tx, "curator_assignment", id, "end", u.ID, "", before, after) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения закрепления")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, after)
}

// Revoke отзывает закрепление с причиной.
func (h *CuratorAssignmentHandlers) Revoke(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canManageCuratorAssignments(u) {
		middleware.WriteError(w, http.StatusForbidden, "изменять закрепления кураторов может администрация")
		return
	}
	var req curatorAssignmentRevokeRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	before, ok := h.load(w, r, tx, u, id)
	if !ok {
		return
	}
	after, err := curators.Revoke(r.Context(), tx, id, u.ID, req.Reason, time.Now())
	if err != nil {
		writeAssignmentError(w, err)
		return
	}
	if logAudit(r.Context(), tx, "curator_assignment", id, "revoke", u.ID, strings.TrimSpace(req.Reason), before, after) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения закрепления")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, after)
}
