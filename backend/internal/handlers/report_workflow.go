package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type ReportWorkflowHandlers struct{ DB *sql.DB }

type reportActivityCheck struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Complete bool   `json:"complete"`
}

type reportAutomaticCheck struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Complete bool   `json:"complete"`
	Detail   string `json:"detail,omitempty"`
}

type reportHistoryItem struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Comment    string    `json:"comment,omitempty"`
	ChangedBy  string    `json:"changed_by"`
	ChangedAt  time.Time `json:"changed_at"`
}

type reportWorkflowResponse struct {
	AgreementID           string                 `json:"agreement_id"`
	ReportYear            int                    `json:"report_year"`
	PeriodType            string                 `json:"period_type"`
	Status                string                 `json:"status"`
	ScopeConfirmed        bool                   `json:"scope_confirmed"`
	ConditionsConfirmed   bool                   `json:"conditions_confirmed"`
	EvidenceConfirmed     bool                   `json:"evidence_confirmed"`
	CounterpartyConfirmed bool                   `json:"counterparty_confirmed"`
	Comment               string                 `json:"comment,omitempty"`
	Activities            []reportActivityCheck  `json:"activities"`
	AutomaticChecks       []reportAutomaticCheck `json:"automatic_checks"`
	Missing               []string               `json:"missing"`
	TopITException        bool                   `json:"top_it_exception"`
	CanMarkReady          bool                   `json:"can_mark_ready"`
	CanVerify             bool                   `json:"can_verify"`
	CanApprove            bool                   `json:"can_approve"`
	CanReturnDraft        bool                   `json:"can_return_draft"`
	ReadyAt               *time.Time             `json:"ready_at,omitempty"`
	VerifiedAt            *time.Time             `json:"verified_at,omitempty"`
	ApprovedAt            *time.Time             `json:"approved_at,omitempty"`
	History               []reportHistoryItem    `json:"history"`
}

type workflowQuerier interface {
	QueryContext(context.Context, string, ...interface{}) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}

func parseReportContext(r *http.Request) (string, int, string, error) {
	q := r.URL.Query()
	agreementID := strings.TrimSpace(q.Get("agreement_id"))
	year, err := strconv.Atoi(q.Get("report_year"))
	period := q.Get("period_type")
	if agreementID == "" || err != nil || year < 2000 || year > 2100 || (period != "plan" && period != "fact") {
		return "", 0, "", fmt.Errorf("укажите соглашение, год и plan/fact")
	}
	return agreementID, year, period, nil
}

func requireAgreement(w http.ResponseWriter, r *http.Request, db *sql.DB, u middleware.AuthUser, agreementID string) bool {
	rows, err := db.QueryContext(r.Context(), `SELECT partner_id::text FROM agreement_partners WHERE agreement_id::text=$1`, agreementID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось проверить доступ к соглашению")
		return false
	}
	defer rows.Close()
	found, allowed := false, false
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			middleware.WriteError(w, 500, "не удалось проверить доступ к соглашению")
			return false
		}
		found = true
		allowed = allowed || canAccessPartner(u, id)
	}
	if !found {
		middleware.WriteError(w, 404, "соглашение не найдено")
		return false
	}
	if !allowed {
		middleware.WriteError(w, 403, "нет доступа к соглашению")
		return false
	}
	return true
}

func topITAlternativeExists(ctx context.Context, q workflowQuerier, agreementID string, year int, period string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT other.id::text
		FROM agreements other
		JOIN agreement_reports report ON report.agreement_id=other.id AND report.report_year=$2 AND report.period_type=$3 AND report.status='approved'
		JOIN agreement_partners op ON op.agreement_id=other.id
		JOIN partners p ON p.id=op.partner_id AND p.partner_kind<>'school'
		WHERE other.id::text<>$1 AND other.status='active'
		AND EXISTS(SELECT 1 FROM agreement_partners current_ap WHERE current_ap.agreement_id::text=$1 AND current_ap.partner_id<>op.partner_id)`, agreementID, year, period)
	if err != nil {
		return false, err
	}
	otherIDs := []string{}
	for rows.Next() {
		var otherID string
		if err = rows.Scan(&otherID); err != nil {
			rows.Close()
			return false, err
		}
		otherIDs = append(otherIDs, otherID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	for _, otherID := range otherIDs {
		var teachers, programs, practice, ministry bool
		if err = q.QueryRowContext(ctx, `SELECT
			COALESCE(bool_or(category_code='teachers'),false),
			COALESCE(bool_or(category_code='ood_rpd'),false),
			COALESCE(bool_or(category_code IN ('internship','employment_practice')),false),
			COALESCE(bool_or(category_code='minc_decision'),false)
			FROM entries WHERE agreement_id::text=$1 AND report_year=$2 AND period_type=$3 AND amount_rub>0`, otherID, year, period).Scan(&teachers, &programs, &practice, &ministry); err != nil {
			return false, err
		}
		if teachers && programs && practice && ministry {
			return true, nil
		}
	}
	return false, nil
}

func buildWorkflow(ctx context.Context, q workflowQuerier, u middleware.AuthUser, agreementID string, year int, period string) (reportWorkflowResponse, error) {
	resp := reportWorkflowResponse{AgreementID: agreementID, ReportYear: year, PeriodType: period, Status: "draft", Activities: []reportActivityCheck{}, AutomaticChecks: []reportAutomaticCheck{}, Missing: []string{}, History: []reportHistoryItem{}}
	var agreementStatus, kind, raStatus string
	var from, until time.Time
	var peopleCP, peopleOther int
	err := q.QueryRowContext(ctx, `SELECT a.status,a.agreement_kind,a.valid_from,a.valid_until,COALESCE(ra.status,''),
		count(rp.id) FILTER(WHERE rp.party='cyberprotect'),count(rp.id) FILTER(WHERE rp.party='counterparty')
		FROM agreements a LEFT JOIN regional_authorities ra ON ra.id=a.regional_authority_id
		LEFT JOIN agreement_responsible_people rp ON rp.agreement_id=a.id WHERE a.id::text=$1
		GROUP BY a.id,ra.status`, agreementID).Scan(&agreementStatus, &kind, &from, &until, &raStatus, &peopleCP, &peopleOther)
	if err != nil {
		return resp, err
	}
	var comment sql.NullString
	var readyAt, verifiedAt, approvedAt sql.NullTime
	err = q.QueryRowContext(ctx, `SELECT status,scope_confirmed,conditions_confirmed,evidence_confirmed,counterparty_confirmed,comment,ready_at,verified_at,approved_at
		FROM agreement_reports WHERE agreement_id::text=$1 AND report_year=$2 AND period_type=$3`, agreementID, year, period).
		Scan(&resp.Status, &resp.ScopeConfirmed, &resp.ConditionsConfirmed, &resp.EvidenceConfirmed, &resp.CounterpartyConfirmed, &comment, &readyAt, &verifiedAt, &approvedAt)
	if err != nil && err != sql.ErrNoRows {
		return resp, err
	}
	if comment.Valid {
		resp.Comment = comment.String
	}
	if readyAt.Valid {
		resp.ReadyAt = &readyAt.Time
	}
	if verifiedAt.Valid {
		resp.VerifiedAt = &verifiedAt.Time
	}
	if approvedAt.Valid {
		resp.ApprovedAt = &approvedAt.Time
	}

	rows, err := q.QueryContext(ctx, `SELECT req.category_code,c.name,EXISTS(SELECT 1 FROM entries e WHERE e.agreement_id=req.agreement_id
		AND e.report_year=$2 AND e.period_type=$3 AND e.category_code=req.category_code AND e.amount_rub>0)
		FROM agreement_activity_requirements req JOIN activity_categories c ON c.code=req.category_code
		WHERE req.agreement_id::text=$1 ORDER BY c.name`, agreementID, year, period)
	if err != nil {
		return resp, err
	}
	required := []string{}
	missingCodes := []string{}
	topComplete := false
	for rows.Next() {
		var a reportActivityCheck
		if err = rows.Scan(&a.Code, &a.Name, &a.Complete); err != nil {
			rows.Close()
			return resp, err
		}
		resp.Activities = append(resp.Activities, a)
		required = append(required, a.Code)
		if !a.Complete {
			missingCodes = append(missingCodes, a.Code)
		}
		if a.Code == "top_it" && a.Complete {
			topComplete = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return resp, err
	}
	rows.Close()
	activitiesComplete := len(required) > 0 && len(missingCodes) == 0
	if !activitiesComplete && kind == "education_organization" && topComplete {
		alternative, altErr := topITAlternativeExists(ctx, q, agreementID, year, period)
		if altErr != nil {
			return resp, altErr
		}
		if alternative {
			activitiesComplete = true
			resp.TopITException = true
		}
	}
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	agreementValid := agreementStatus == "active" && !from.After(yearEnd) && !until.Before(yearStart) && (kind != "roiv" || raStatus == "active")
	peopleValid := peopleCP > 0 && peopleOther > 0
	resp.AutomaticChecks = []reportAutomaticCheck{
		{Code: "agreement", Label: "Соглашение действует в отчётном году и надлежаще подписано", Complete: agreementValid},
		{Code: "responsible_people", Label: "Указаны ответственные обеих сторон", Complete: peopleValid},
		{Code: "activity_scope", Label: "Перечень видов мероприятий задан в соглашении", Complete: len(required) > 0},
		{Code: "activities", Label: "По каждому виду есть мероприятие с положительной расчётной суммой", Complete: activitiesComplete},
	}
	for _, check := range resp.AutomaticChecks {
		if !check.Complete {
			resp.Missing = append(resp.Missing, check.Label)
		}
	}
	if !activitiesComplete {
		for _, a := range resp.Activities {
			if !a.Complete {
				resp.Missing = append(resp.Missing, a.Name)
			}
		}
	}
	manualComplete := resp.ScopeConfirmed && resp.ConditionsConfirmed && resp.EvidenceConfirmed && (period == "plan" || resp.CounterpartyConfirmed)
	if !resp.ScopeConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждено соответствие конкретному перечню, объёму, срокам и условиям соглашения")
	}
	if !resp.ConditionsConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждены условия реализации видов мероприятий по приложению № 1")
	}
	if !resp.EvidenceConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждено наличие однозначных подтверждающих документов")
	}
	if period == "fact" && !resp.CounterpartyConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждено рассмотрение перечня образовательной организацией или РОИВ")
	}
	automaticComplete := true
	for _, check := range resp.AutomaticChecks {
		automaticComplete = automaticComplete && check.Complete
	}
	resp.CanMarkReady = resp.Status == "draft" && automaticComplete && manualComplete
	resp.CanVerify = resp.Status == "ready" && isStaff(u)
	resp.CanApprove = resp.Status == "verified" && u.Role == models.RoleAdmin
	resp.CanReturnDraft = resp.Status != "draft" && (isStaff(u) || resp.Status == "ready")
	historyRows, historyErr := q.QueryContext(ctx, `SELECT h.from_status,h.to_status,COALESCE(h.comment,''),users.full_name,h.changed_at
		FROM agreement_report_history h JOIN users ON users.id=h.changed_by
		WHERE h.agreement_id::text=$1 AND h.report_year=$2 AND h.period_type=$3
		ORDER BY h.changed_at DESC,h.id DESC LIMIT 50`, agreementID, year, period)
	if historyErr != nil {
		return resp, historyErr
	}
	for historyRows.Next() {
		var item reportHistoryItem
		if historyErr = historyRows.Scan(&item.FromStatus, &item.ToStatus, &item.Comment, &item.ChangedBy, &item.ChangedAt); historyErr != nil {
			historyRows.Close()
			return resp, historyErr
		}
		resp.History = append(resp.History, item)
	}
	historyErr = historyRows.Err()
	historyRows.Close()
	if historyErr != nil {
		return resp, historyErr
	}
	return resp, nil
}

func (h *ReportWorkflowHandlers) Get(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	id, year, period, err := parseReportContext(r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if !requireAgreement(w, r, h.DB, u, id) {
		return
	}
	resp, err := buildWorkflow(r.Context(), h.DB, u, id, year, period)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка проверки готовности отчёта")
		return
	}
	middleware.WriteJSON(w, 200, resp)
}

type workflowTransitionRequest struct {
	Status                string `json:"status"`
	ScopeConfirmed        bool   `json:"scope_confirmed"`
	ConditionsConfirmed   bool   `json:"conditions_confirmed"`
	EvidenceConfirmed     bool   `json:"evidence_confirmed"`
	CounterpartyConfirmed bool   `json:"counterparty_confirmed"`
	Comment               string `json:"comment"`
}

func (h *ReportWorkflowHandlers) Transition(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	id, year, period, err := parseReportContext(r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if !requireAgreement(w, r, h.DB, u, id) {
		return
	}
	var req workflowTransitionRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req.Comment = strings.TrimSpace(req.Comment)
	if len([]rune(req.Comment)) > 1000 {
		middleware.WriteError(w, 400, "комментарий не должен превышать 1000 символов")
		return
	}
	if req.Comment == "" {
		middleware.WriteError(w, 400, "укажите комментарий-основание для смены статуса")
		return
	}
	if req.Status != "draft" && req.Status != "ready" && req.Status != "verified" && req.Status != "approved" {
		middleware.WriteError(w, 400, "некорректный статус отчёта")
		return
	}
	if !isStaff(u) {
		var partnerCount int
		if err = h.DB.QueryRowContext(r.Context(), `SELECT count(*) FROM agreement_partners WHERE agreement_id::text=$1`, id).Scan(&partnerCount); err != nil {
			middleware.WriteError(w, 500, "ошибка проверки состава соглашения")
			return
		}
		if partnerCount != 1 {
			middleware.WriteError(w, 403, "статус отчёта по соглашению с несколькими организациями меняет сотрудник Киберпротекта")
			return
		}
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO agreement_reports(agreement_id,report_year,period_type,updated_by) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, year, period, u.ID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка создания карточки отчёта")
		return
	}
	var current string
	if err = tx.QueryRowContext(r.Context(), `SELECT status FROM agreement_reports WHERE agreement_id=$1 AND report_year=$2 AND period_type=$3 FOR UPDATE`, id, year, period).Scan(&current); err != nil {
		middleware.WriteError(w, 500, "ошибка блокировки отчёта")
		return
	}
	allowed := false
	switch req.Status {
	case "ready":
		allowed = current == "draft"
	case "verified":
		allowed = current == "ready" && isStaff(u)
	case "approved":
		allowed = current == "verified" && u.Role == models.RoleAdmin
	case "draft":
		allowed = current != "draft" && (isStaff(u) || current == "ready")
	}
	if !allowed {
		middleware.WriteError(w, 409, "недопустимый переход статуса или недостаточно прав")
		return
	}
	if req.Status == "ready" {
		_, err = tx.ExecContext(r.Context(), `UPDATE agreement_reports SET scope_confirmed=$4,conditions_confirmed=$5,evidence_confirmed=$6,counterparty_confirmed=$7,comment=NULLIF($8,''),updated_by=$9,updated_at=now() WHERE agreement_id=$1 AND report_year=$2 AND period_type=$3`, id, year, period, req.ScopeConfirmed, req.ConditionsConfirmed, req.EvidenceConfirmed, req.CounterpartyConfirmed, req.Comment, u.ID)
		if err != nil {
			middleware.WriteError(w, 500, "ошибка сохранения подтверждений")
			return
		}
		check, checkErr := buildWorkflow(r.Context(), tx, u, id, year, period)
		if checkErr != nil {
			middleware.WriteError(w, 500, "ошибка проверки отчёта")
			return
		}
		if !check.CanMarkReady {
			middleware.WriteJSON(w, 422, map[string]interface{}{"error": "отчёт не готов: устраните все замечания и подтвердите юридические условия", "workflow": check})
			return
		}
	} else if req.Status != "draft" {
		check, checkErr := buildWorkflow(r.Context(), tx, u, id, year, period)
		if checkErr != nil {
			middleware.WriteError(w, 500, "ошибка повторной проверки отчёта")
			return
		}
		manualOK := check.ScopeConfirmed && check.ConditionsConfirmed && check.EvidenceConfirmed && (period == "plan" || check.CounterpartyConfirmed)
		if len(check.Missing) > 0 || !manualOK {
			middleware.WriteJSON(w, 422, map[string]interface{}{"error": "переход запрещён: комплектность или юридические подтверждения больше не действуют", "workflow": check})
			return
		}
	}
	stamp := "updated_at=now()"
	switch req.Status {
	case "ready":
		stamp += ",ready_by=$3,ready_at=now(),verified_by=NULL,verified_at=NULL,approved_by=NULL,approved_at=NULL"
	case "verified":
		stamp += ",verified_by=$3,verified_at=now(),approved_by=NULL,approved_at=NULL"
	case "approved":
		stamp += ",approved_by=$3,approved_at=now()"
	case "draft":
		stamp += ",scope_confirmed=FALSE,conditions_confirmed=FALSE,evidence_confirmed=FALSE,counterparty_confirmed=FALSE,ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,approved_by=NULL,approved_at=NULL"
	}
	query := `UPDATE agreement_reports SET status=$1,comment=NULLIF($2,''),updated_by=$3,` + stamp + ` WHERE agreement_id=$4 AND report_year=$5 AND period_type=$6`
	if _, err = tx.ExecContext(r.Context(), query, req.Status, req.Comment, u.ID, id, year, period); err != nil {
		middleware.WriteError(w, 500, "ошибка смены статуса")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO agreement_report_history(agreement_id,report_year,period_type,from_status,to_status,comment,changed_by) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7)`, id, year, period, current, req.Status, req.Comment, u.ID); err != nil {
		middleware.WriteError(w, 500, "ошибка истории статусов")
		return
	}
	if err = logAudit(tx, "report", id, "status", u.ID, req.Comment, map[string]string{"status": current}, map[string]string{"status": req.Status}); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err = tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения статуса")
		return
	}
	resp, err := buildWorkflow(r.Context(), h.DB, u, id, year, period)
	if err != nil {
		middleware.WriteError(w, 500, "статус сохранён, но не удалось обновить карточку")
		return
	}
	middleware.WriteJSON(w, 200, resp)
}
