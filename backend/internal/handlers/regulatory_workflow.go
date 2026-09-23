package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	workflowdomain "cybercalc/internal/workflow"
)

type RegulatoryWorkflowHandlers struct{ DB *sql.DB }

type regulatoryWorkflowStartInput struct {
	AgreementID string `json:"agreement_id"`
	ReportYear  int    `json:"report_year"`
	ProcessType string `json:"process_type"`
}

type regulatoryWorkflowTransitionInput struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	ReviewDueOn string `json:"review_due_on"`
	Version     int64  `json:"version"`
}

func (h *RegulatoryWorkflowHandlers) Get(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	agreementID := strings.TrimSpace(r.URL.Query().Get("agreement_id"))
	processType := strings.TrimSpace(r.URL.Query().Get("process_type"))
	year, err := strconv.Atoi(r.URL.Query().Get("report_year"))
	if agreementID == "" || !workflowdomain.ValidProcess(processType) || err != nil || year < 2000 || year > 2100 {
		middleware.WriteError(w, 400, "укажите agreement_id, report_year и process_type")
		return
	}
	if !requireAgreement(w, r, h.DB, u, agreementID) {
		return
	}
	item, err := h.read(r, `agreement_id::text=$1 AND report_year=$2 AND process_type=$3`, agreementID, year, processType)
	if errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, 404, "регламентный процесс не создан")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения процесса")
		return
	}
	middleware.WriteJSON(w, 200, item)
}

func (h *RegulatoryWorkflowHandlers) Start(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canPrepareReports(u) {
		middleware.WriteError(w, 403, "роль не может запускать регламентный процесс")
		return
	}
	var input regulatoryWorkflowStartInput
	if decodeJSON(r, &input) != nil || !workflowdomain.ValidProcess(input.ProcessType) || input.ReportYear < 2000 || input.ReportYear > 2100 {
		middleware.WriteError(w, 400, "некорректный регламентный процесс")
		return
	}
	if !requireAgreement(w, r, h.DB, u, input.AgreementID) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO regulatory_workflows(agreement_id,report_year,process_type,updated_by)
		VALUES($1,$2,$3,$4) RETURNING id::text`, input.AgreementID, input.ReportYear, input.ProcessType, u.ID).Scan(&id)
	if err != nil {
		middleware.WriteError(w, 409, "регламентный процесс уже существует")
		return
	}
	if err := logAudit(r.Context(), tx, "regulatory_workflow", id, "create", u.ID, "", nil, input); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения процесса")
		return
	}
	middleware.WriteJSON(w, 201, map[string]interface{}{"id": id, "status": workflowdomain.Draft, "version": 1})
}

func (h *RegulatoryWorkflowHandlers) Transition(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	var input regulatoryWorkflowTransitionInput
	if decodeJSON(r, &input) != nil || input.Version < 1 {
		middleware.WriteError(w, 400, "укажите статус и текущую версию процесса")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		middleware.WriteError(w, 400, "причина перехода обязательна")
		return
	}
	to := workflowdomain.Status(input.Status)
	if !workflowdomain.ValidStatus(to) {
		middleware.WriteError(w, 400, "неизвестный статус процесса")
		return
	}
	var due interface{}
	if input.ReviewDueOn != "" {
		parsed, err := time.Parse("2006-01-02", input.ReviewDueOn)
		if err != nil {
			middleware.WriteError(w, 400, "review_due_on должен быть датой YYYY-MM-DD")
			return
		}
		due = parsed
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var agreementID string
	var from workflowdomain.Status
	var version int64
	if err := tx.QueryRowContext(r.Context(), `SELECT agreement_id::text,status,version FROM regulatory_workflows WHERE id::text=$1 FOR UPDATE`, id).Scan(&agreementID, &from, &version); errors.Is(err, sql.ErrNoRows) {
		middleware.WriteError(w, 404, "регламентный процесс не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения процесса")
		return
	}
	if !requireAgreement(w, r, h.DB, u, agreementID) {
		return
	}
	if version != input.Version {
		middleware.WriteError(w, 409, "процесс уже изменён; обновите карточку")
		return
	}
	if err := workflowdomain.ValidateTransition(from, to); err != nil {
		middleware.WriteError(w, 409, err.Error())
		return
	}
	operatorTransition := to == workflowdomain.Sent || to == workflowdomain.Resubmitted
	if operatorTransition && !canPrepareReports(u) {
		middleware.WriteError(w, 403, "роль не может направить пакет")
		return
	}
	if !operatorTransition && !canReviewReport(u, "fact") && !canApproveReports(u) {
		middleware.WriteError(w, 403, "роль не может рассматривать пакет")
		return
	}
	if to == workflowdomain.DefaultApproved && !canApproveReports(u) {
		middleware.WriteError(w, 403, "автоматическое одобрение фиксирует только уполномоченный администратор")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE regulatory_workflows SET status=$1,version=version+1,
		sent_at=CASE WHEN $1 IN ('sent','resubmitted') THEN COALESCE(sent_at,now()) ELSE sent_at END,
		review_due_on=COALESCE($2,review_due_on),decided_at=CASE WHEN $1 IN ('approved','default_approved','disputed') THEN now() ELSE NULL END,
		updated_by=$3,updated_at=now() WHERE id::text=$4`, string(to), due, u.ID, id)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения процесса")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO regulatory_workflow_history(workflow_id,from_status,to_status,reason,changed_by)
		VALUES($1,$2,$3,$4,$5)`, id, string(from), string(to), input.Reason, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка истории процесса")
		return
	}
	if err := logAudit(r.Context(), tx, "regulatory_workflow", id, "transition", u.ID, input.Reason,
		map[string]interface{}{"status": from, "version": version}, map[string]interface{}{"status": to, "version": version + 1}); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения процесса")
		return
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"id": id, "status": to, "version": version + 1})
}

func (h *RegulatoryWorkflowHandlers) read(r *http.Request, where string, args ...interface{}) (map[string]interface{}, error) {
	var id, agreementID, processType, status, due string
	var year int
	var version int64
	var sentAt, decidedAt sql.NullTime
	var updatedAt time.Time
	err := h.DB.QueryRowContext(r.Context(), `SELECT id::text,agreement_id::text,report_year,process_type,status,version,
		COALESCE(review_due_on::text,''),sent_at,decided_at,updated_at FROM regulatory_workflows WHERE `+where, args...).Scan(
		&id, &agreementID, &year, &processType, &status, &version, &due, &sentAt, &decidedAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "agreement_id": agreementID, "report_year": year,
		"process_type": processType, "status": status, "version": version, "review_due_on": due,
		"sent_at": sentAt, "decided_at": decidedAt, "updated_at": updatedAt}, nil
}
