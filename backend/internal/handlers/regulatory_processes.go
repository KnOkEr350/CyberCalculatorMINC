package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/regulatory"
)

// RegulatoryHandlers — регламентные процессы Приказа № 270: проект
// соглашения, предварительный и итоговый перечни (WF-01, WF-03–WF-05).
type RegulatoryHandlers struct {
	DB *sql.DB
}

type processView struct {
	ID            string      `json:"id"`
	Kind          string      `json:"kind"`
	AgreementID   string      `json:"agreement_id"`
	ReportYear    int         `json:"report_year"`
	Status        string      `json:"status"`
	Window        string      `json:"window,omitempty"`
	DueDate       string      `json:"due_date,omitempty"`
	DaysLeft      *int        `json:"days_left,omitempty"`
	Round         int         `json:"round"`
	SentAt        string      `json:"sent_at,omitempty"`
	SentLate      bool        `json:"sent_late"`
	Remarks       string      `json:"remarks,omitempty"`
	ReworkOverdue bool        `json:"rework_overdue"`
	Cutoff        string      `json:"cutoff_date,omitempty"`
	SendDue       string      `json:"send_due_date,omitempty"`
	Allowed       []string    `json:"allowed_actions"`
	Events        []eventView `json:"events,omitempty"`
}

type eventView struct {
	Action  string `json:"action"`
	From    string `json:"from_status"`
	To      string `json:"to_status"`
	Reason  string `json:"reason,omitempty"`
	DueDate string `json:"due_date,omitempty"`
	Actor   string `json:"actor_id,omitempty"`
	System  bool   `json:"system"`
	At      string `json:"occurred_at"`
}

// processAccess — как пользователь связан с соглашением процесса.
type processAccess struct {
	author   bool // подготовка и отправка: ИТ-организация соглашения
	reviewer bool // рассмотрение: образовательная организация соглашения
	onBehalf bool // сотрудник ИТ-организации, фиксирующий ответ контрагента
	read     bool
}

// accessToAgreement определяет права пользователя на процессы соглашения.
func (h *RegulatoryHandlers) accessToAgreement(ctx context.Context, u middleware.AuthUser, agreementID string) (processAccess, error) {
	var company sql.NullString
	err := h.DB.QueryRowContext(ctx, `SELECT it_company_id::text FROM agreements WHERE id::text=$1`, agreementID).Scan(&company)
	if err != nil {
		return processAccess{}, err
	}
	var access processAccess
	switch u.EntityType {
	case models.EntityEduInst:
		if u.PartnerID == nil || *u.PartnerID == "" {
			return access, nil
		}
		var linked bool
		if err := h.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agreement_partners
			WHERE agreement_id::text=$1 AND partner_id::text=$2)`, agreementID, *u.PartnerID).Scan(&linked); err != nil {
			return access, err
		}
		if linked && isEducationRepresentative(u) {
			access.reviewer, access.read = true, true
		}
	case models.EntityOrganization:
		if u.ITCompanyID == nil || !company.Valid || *u.ITCompanyID != company.String || !canReadTenantData(u) {
			return access, nil
		}
		access.read = true
		access.author = canPrepareReports(u)
		access.onBehalf = canReviewReport(u, string(models.PeriodFact))
	}
	return access, nil
}

// authorizedActions — какие действия из допустимых процессом доступны
// пользователю. Автор отправляет и дорабатывает, рецензент отвечает;
// сотрудник ИТ-организации может зафиксировать ответ контрагента, но только
// с указанием основания.
func authorizedActions(state regulatory.State, access processAccess, now time.Time) []string {
	out := []string{}
	for _, action := range regulatory.Allowed(state, now) {
		switch action {
		case regulatory.ActionSend, regulatory.ActionResubmit:
			if access.author {
				out = append(out, string(action))
			}
		default:
			if access.reviewer || access.onBehalf {
				out = append(out, string(action))
			}
		}
	}
	return out
}

func toView(process regulatory.Process, access processAccess, now time.Time, events []regulatory.StoredEvent) processView {
	state := process.State
	view := processView{
		ID: process.ID, Kind: string(state.Kind), AgreementID: process.AgreementID, ReportYear: state.ReportYear,
		Status: string(state.Status), Window: string(state.Window), DueDate: state.DueDate, Round: state.Round,
		SentLate: state.SentLate, Remarks: state.Remarks,
		ReworkOverdue: state.Status == regulatory.StatusRework && regulatory.DeadlineExpired(state.ExpiresAt, now),
		Allowed:       authorizedActions(state, access, now),
	}
	if state.SentAt != nil {
		view.SentAt = state.SentAt.In(businessLocation).Format(time.RFC3339)
	}
	if state.DueDate != "" {
		if due, err := time.ParseInLocation("2006-01-02", state.DueDate, businessLocation); err == nil {
			days := int(due.Sub(regulatory.Today(now)).Hours() / 24)
			view.DaysLeft = &days
		}
	}
	if milestones := regulatory.MilestonesFor(state.Kind, state.ReportYear); milestones.Has {
		view.Cutoff, view.SendDue = milestones.Cutoff.Format("2006-01-02"), milestones.SendDue.Format("2006-01-02")
	}
	for _, event := range events {
		view.Events = append(view.Events, eventView{
			Action: string(event.Action), From: string(event.From), To: string(event.To), Reason: event.Reason,
			DueDate: event.DueDate, Actor: event.ActorID, System: event.System, At: event.At.In(businessLocation).Format(time.RFC3339),
		})
	}
	return view
}

type createProcessRequest struct {
	Kind        string `json:"kind"`
	AgreementID string `json:"agreement_id"`
	ReportYear  int    `json:"report_year"`
}

// Create заводит процесс в черновике. Повторный вызов возвращает уже
// существующий процесс (200), а не создаёт второй.
func (h *RegulatoryHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req createProcessRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.AgreementID = strings.TrimSpace(req.AgreementID)
	if _, err := regulatory.New(regulatory.Kind(req.Kind), req.ReportYear); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	access, err := h.accessToAgreement(r.Context(), u, req.AgreementID)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, http.StatusNotFound, "соглашение не найдено")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить доступ")
		return
	}
	if !access.author {
		middleware.WriteError(w, http.StatusForbidden, "процесс заводит ИТ-организация соглашения")
		return
	}
	process, err := regulatory.Create(r.Context(), h.DB, regulatory.Kind(req.Kind), req.AgreementID, req.ReportYear, u.ID)
	status := http.StatusCreated
	if errors.Is(err, regulatory.ErrExists) {
		status, err = http.StatusOK, nil
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось создать процесс")
		return
	}
	middleware.WriteJSON(w, status, toView(process, access, time.Now(), nil))
}

// List отдаёт процессы соглашения.
func (h *RegulatoryHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	agreementID := strings.TrimSpace(r.URL.Query().Get("agreement_id"))
	if agreementID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите agreement_id")
		return
	}
	access, err := h.accessToAgreement(r.Context(), u, agreementID)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, http.StatusNotFound, "соглашение не найдено")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить доступ")
		return
	}
	if !access.read {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к процессам этого соглашения")
		return
	}
	year := 0
	if value := r.URL.Query().Get("report_year"); value != "" {
		if year, err = strconv.Atoi(value); err != nil || year < 2000 || year > 2100 {
			middleware.WriteError(w, http.StatusBadRequest, "некорректный год")
			return
		}
	}
	kind := r.URL.Query().Get("kind")
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id::text FROM regulatory_processes
		WHERE agreement_id::text=$1 AND ($2=0 OR report_year=$2) AND ($3='' OR kind=$3)
		ORDER BY report_year DESC,kind`, agreementID, year, kind)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения процессов")
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения процессов")
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	now := time.Now()
	out := make([]processView, 0, len(ids))
	for _, id := range ids {
		process, err := regulatory.Get(r.Context(), h.DB, id)
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения процесса")
			return
		}
		out = append(out, toView(process, access, now, nil))
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

// Get отдаёт процесс с историей переходов.
func (h *RegulatoryHandlers) Get(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	process, err := regulatory.Get(r.Context(), h.DB, id)
	if errors.Is(err, regulatory.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "процесс не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения процесса")
		return
	}
	access, err := h.accessToAgreement(r.Context(), u, process.AgreementID)
	if err != nil || !access.read {
		// Чужой процесс неотличим от несуществующего.
		middleware.WriteError(w, http.StatusNotFound, "процесс не найден")
		return
	}
	events, err := regulatory.Events(r.Context(), h.DB, id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, toView(process, access, time.Now(), events))
}

type processActionRequest struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// Act выполняет действие над процессом.
func (h *RegulatoryHandlers) Act(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	var req processActionRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	action := regulatory.Action(req.Action)
	known := false
	for _, candidate := range regulatory.Actions() {
		known = known || candidate == action
	}
	if !known {
		middleware.WriteError(w, http.StatusBadRequest, "неизвестное действие")
		return
	}
	process, err := regulatory.Get(r.Context(), h.DB, id)
	if errors.Is(err, regulatory.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "процесс не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения процесса")
		return
	}
	access, err := h.accessToAgreement(r.Context(), u, process.AgreementID)
	if err != nil || !access.read {
		middleware.WriteError(w, http.StatusNotFound, "процесс не найден")
		return
	}
	// Право на действие проверяется по роли до обращения к процессу: отказ не
	// должен зависеть от его состояния и раскрывать его.
	author := action == regulatory.ActionSend || action == regulatory.ActionResubmit
	switch {
	case author && !access.author:
		middleware.WriteError(w, http.StatusForbidden, "отправляет и дорабатывает ИТ-организация соглашения")
		return
	case !author && !access.reviewer && !access.onBehalf:
		middleware.WriteError(w, http.StatusForbidden, "отвечает образовательная организация соглашения")
		return
	}
	// Ответ за контрагента фиксирует сотрудник ИТ-организации: без основания
	// (реквизитов полученного ответа) такая запись ничего не подтверждает, а
	// отправитель мог бы согласовать собственный перечень молча.
	reason := strings.TrimSpace(req.Reason)
	if !author && !access.reviewer && reason == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите основание: реквизиты ответа контрагента")
		return
	}

	now := time.Now()
	updated, applied, err := regulatory.Act(r.Context(), h.DB, id, action, u.ID, reason, now,
		func(ctx context.Context, tx *sql.Tx, events []regulatory.StoredEvent) error {
			for _, event := range events {
				actor := u.ID
				if event.System {
					actor = ""
				}
				if err := logAudit(ctx, tx, "regulatory_process", id, "process_"+string(event.Action), actor, event.Reason,
					map[string]string{"status": string(event.From)}, map[string]string{"status": string(event.To)}); err != nil {
					return err
				}
			}
			return nil
		})
	switch {
	case errors.Is(err, regulatory.ErrInvalidTransition), errors.Is(err, regulatory.ErrWindowClosed):
		middleware.WriteError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, regulatory.ErrTooEarly), errors.Is(err, regulatory.ErrReasonRequired):
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, regulatory.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, "процесс не найден")
		return
	case err != nil:
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось выполнить действие")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, toView(updated, access, now, applied))
}
