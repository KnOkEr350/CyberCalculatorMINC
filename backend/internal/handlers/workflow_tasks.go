package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
	"cybercalc/internal/tasks"
)

// WorkflowTaskHandlers — задачи workflow и диспетчер эскалаций (SEC-10).
type WorkflowTaskHandlers struct{ DB *sql.DB }

type createTaskRequest struct {
	ITCompanyID        string `json:"it_company_id,omitempty"`
	PartnerID          string `json:"partner_id,omitempty"`
	Kind               string `json:"kind"`
	Title              string `json:"title"`
	SubjectType        string `json:"subject_type"`
	SubjectID          string `json:"subject_id"`
	RequiredRole       string `json:"required_role"`
	RequiredPermission string `json:"required_permission"`
}

type reassignTaskRequest struct {
	Reason string `json:"reason"`
}

func canDispatchTasks(u middleware.AuthUser) bool {
	return u.EntityType == models.EntityOrganization && rbac.Allows(u.Role, rbac.DispatchTasks)
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, tasks.ErrNotOpen):
		middleware.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, tasks.ErrInvalid), errors.Is(err, tasks.ErrPartnerTenant):
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, tasks.ErrNotAssignee), errors.Is(err, tasks.ErrNotDelegated):
		middleware.WriteError(w, http.StatusForbidden, err.Error())
	default:
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка задач")
	}
}

// visible — управляющий видит задачи своего арендатора, остальные — только свои.
func visibleTask(u middleware.AuthUser, t tasks.Task) bool {
	if t.AssigneeID == u.ID {
		return true
	}
	if !canDispatchTasks(u) {
		return false
	}
	return assignmentTenant(u) == "" || assignmentTenant(u) == t.ITCompanyID
}

// List — свои задачи; управляющему — задачи арендатора, в том числе
// эскалированные и без исполнителя.
func (h *WorkflowTaskHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	filter := tasks.Filter{Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Escalated: r.URL.Query().Get("escalated") == "1" || r.URL.Query().Get("escalated") == "true"}
	if filter.Status != "" && filter.Status != "open" && filter.Status != "done" && filter.Status != "cancelled" {
		middleware.WriteError(w, http.StatusBadRequest, "статус: open, done или cancelled")
		return
	}
	if canDispatchTasks(u) {
		filter.TenantID = assignmentTenant(u)
		filter.AssigneeID = strings.TrimSpace(r.URL.Query().Get("assignee_id"))
	} else {
		filter.AssigneeID = u.ID
	}
	list, err := tasks.List(r.Context(), h.DB, filter)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, list)
}

// Get — задача с историей маршрута, причин fallback и попыток переназначения.
func (h *WorkflowTaskHandlers) Get(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	task, err := tasks.Get(r.Context(), h.DB, id)
	if err != nil || !visibleTask(u, task) {
		if err == nil {
			err = tasks.ErrNotFound
		}
		writeTaskError(w, err)
		return
	}
	history, err := tasks.History(r.Context(), h.DB, id)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"task": task, "history": history})
}

// Create ставит задачу; исполнителя выбирает диспетчер.
func (h *WorkflowTaskHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canDispatchTasks(u) {
		middleware.WriteError(w, http.StatusForbidden, "ставить задачи может администрация")
		return
	}
	var req createTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	tenant := assignmentTenant(u)
	if tenant == "" {
		tenant = strings.TrimSpace(req.ITCompanyID)
	} else if req.ITCompanyID != "" && req.ITCompanyID != tenant {
		middleware.WriteError(w, http.StatusNotFound, "ИТ-компания не найдена")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	task, created, err := tasks.Dispatch(r.Context(), tx, tasks.NewTask{TenantID: tenant, PartnerID: strings.TrimSpace(req.PartnerID),
		Kind: strings.TrimSpace(req.Kind), Title: req.Title, SubjectType: strings.TrimSpace(req.SubjectType), SubjectID: req.SubjectID,
		Role: models.Role(strings.TrimSpace(req.RequiredRole)), Permission: rbac.Permission(strings.TrimSpace(req.RequiredPermission)),
		CreatedBy: u.ID}, time.Now())
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if created {
		if logAudit(r.Context(), tx, "workflow_task", task.ID, "create", u.ID, task.FallbackReason, nil, task) != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка аудита")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	middleware.WriteJSON(w, status, task)
}

// Reassign — ручная попытка переназначения: цепочка проходится заново.
func (h *WorkflowTaskHandlers) Reassign(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canDispatchTasks(u) {
		middleware.WriteError(w, http.StatusForbidden, "переназначать задачи может администрация")
		return
	}
	var req reassignTaskRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите причину переназначения")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	before, err := tasks.Get(r.Context(), tx, id)
	if err != nil || !visibleTask(u, before) {
		if err == nil {
			err = tasks.ErrNotFound
		}
		writeTaskError(w, err)
		return
	}
	after, moved, err := tasks.Reassign(r.Context(), tx, id, u.ID, req.Reason, time.Now())
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if logAudit(r.Context(), tx, "workflow_task", id, "reassign", u.ID, strings.TrimSpace(req.Reason), before, after) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"task": after, "reassigned": moved})
}

// Complete закрывает задачу. Исполнитель действует в пределах своих
// полномочий; куратору по маршруту fallback доступны только делегированные.
func (h *WorkflowTaskHandlers) Complete(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	task, err := tasks.Get(r.Context(), tx, id)
	if err != nil || !visibleTask(u, task) {
		if err == nil {
			err = tasks.ErrNotFound
		}
		writeTaskError(w, err)
		return
	}
	if task.AssigneeID != u.ID {
		writeTaskError(w, tasks.ErrNotAssignee)
		return
	}
	if !tasks.CanExecute(u.Role, true, task.Route, rbac.Permission(task.RequiredPermission)) {
		if task.Route == tasks.RouteCurator {
			writeTaskError(w, tasks.ErrNotDelegated)
		} else {
			writeTaskError(w, tasks.ErrNotAssignee)
		}
		return
	}
	done, err := tasks.Complete(r.Context(), tx, id, u.ID)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	if logAudit(r.Context(), tx, "workflow_task", id, "complete", u.ID, string(task.Route), task, done) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, done)
}
