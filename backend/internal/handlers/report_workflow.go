package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"github.com/lib/pq"
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
	AgreementID            string                 `json:"agreement_id"`
	ReportYear             int                    `json:"report_year"`
	PeriodType             string                 `json:"period_type"`
	Status                 string                 `json:"status"`
	ScopeConfirmed         bool                   `json:"scope_confirmed"`
	ConditionsConfirmed    bool                   `json:"conditions_confirmed"`
	EvidenceConfirmed      bool                   `json:"evidence_confirmed"`
	ActualCostsConfirmed   bool                   `json:"actual_costs_confirmed"`
	AuditorReportReference string                 `json:"auditor_report_reference,omitempty"`
	UsesActualCosts        bool                   `json:"uses_actual_costs"`
	CounterpartyConfirmed  bool                   `json:"counterparty_confirmed"`
	Comment                string                 `json:"comment,omitempty"`
	Activities             []reportActivityCheck  `json:"activities"`
	AutomaticChecks        []reportAutomaticCheck `json:"automatic_checks"`
	Missing                []string               `json:"missing"`
	HigherEducation        bool                   `json:"higher_education"` // ADR-11: обязательный минимум Видов 1 и 3 применяется только к ВО
	TopITException         bool                   `json:"top_it_exception"`
	TopITBasis             *clause22Basis         `json:"top_it_exception_basis,omitempty"` // ADR-03: чем именно подтверждено освобождение
	CanMarkReady           bool                   `json:"can_mark_ready"`
	CanVerify              bool                   `json:"can_verify"`
	CanApprove             bool                   `json:"can_approve"`
	CanReturnDraft         bool                   `json:"can_return_draft"`
	ReadyAt                *time.Time             `json:"ready_at,omitempty"`
	VerifiedAt             *time.Time             `json:"verified_at,omitempty"`
	ApprovedAt             *time.Time             `json:"approved_at,omitempty"`
	ReviewDueDate          string                 `json:"review_due_date,omitempty"` // последний календарный день срока, YYYY-MM-DD (Москва)
	ReviewDueAt            *time.Time             `json:"review_due_at,omitempty"`   // момент истечения срока: начало следующего дня по Москве
	ReviewDays             int                    `json:"review_calendar_days"`
	ReviewOverdue          bool                   `json:"review_overdue"`
	History                []reportHistoryItem    `json:"history"`
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
	rows, err := db.QueryContext(r.Context(), `SELECT ap.partner_id::text,COALESCE(a.it_company_id::text,'') FROM agreement_partners ap JOIN agreements a ON a.id=ap.agreement_id WHERE ap.agreement_id::text=$1`, agreementID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось проверить доступ к соглашению")
		return false
	}
	defer rows.Close()
	found, allowed := false, false
	for rows.Next() {
		var id, companyID string
		if rows.Scan(&id, &companyID) != nil {
			middleware.WriteError(w, 500, "не удалось проверить доступ к соглашению")
			return false
		}
		found = true
		tenantAllowed := u.ITCompanyID == nil || *u.ITCompanyID == companyID
		if u.EntityType == "organization" && u.ITCompanyID == nil {
			tenantAllowed = false
		}
		allowed = allowed || (canAccessPartner(u, id) && tenantAllowed)
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

// clause22Basis — обоснование освобождения по п. 22: конкретная иная ОО «B»
// и записи обязательных Видов 1 и 3, на которых оно держится. ADR-03 требует
// показывать их, чтобы освобождение можно было проверить, а не принимать на
// слово.
type clause22Basis struct {
	AgreementID     string          `json:"agreement_id"`
	AgreementNumber string          `json:"agreement_number"`
	PartnerName     string          `json:"partner_name"`
	Records         []clause22Entry `json:"records"`
}

type clause22Entry struct {
	CategoryCode string       `json:"category_code"`
	CategoryName string       `json:"category_name"`
	EntryCount   int          `json:"entry_count"`
	AmountRub    money.Amount `json:"amount_rub"`
}

// topITAlternativeBasis ищет иную ОО, закрывающую обязательные Виды 1 и 3, и
// возвращает обоснование освобождения. nil означает, что освобождения нет.
func topITAlternativeBasis(ctx context.Context, q workflowQuerier, agreementID string, year int, period string) (*clause22Basis, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT other.id::text,other.number,p.name
		FROM agreements other
		JOIN agreement_reports report ON report.agreement_id=other.id AND report.report_year=$2 AND report.period_type=$3 AND report.status='approved'
		JOIN agreement_partners op ON op.agreement_id=other.id
		JOIN partners p ON p.id=op.partner_id AND p.partner_kind<>'school'
		WHERE other.id::text<>$1 AND other.status='active'
		AND other.it_company_id=(SELECT it_company_id FROM agreements WHERE id::text=$1)
		AND EXISTS(SELECT 1 FROM agreement_partners current_ap WHERE current_ap.agreement_id::text=$1 AND current_ap.partner_id<>op.partner_id)
		ORDER BY p.name,other.number`, agreementID, year, period)
	if err != nil {
		return nil, err
	}
	candidates := []clause22Basis{}
	for rows.Next() {
		var candidate clause22Basis
		if err = rows.Scan(&candidate.AgreementID, &candidate.AgreementNumber, &candidate.PartnerName); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, candidate := range candidates {
		records, teachers, programs, recErr := clause22Records(ctx, q, candidate.AgreementID, year, period)
		if recErr != nil {
			return nil, recErr
		}
		if clause22AlternativeSatisfied(teachers, programs) {
			candidate.Records = records
			return &candidate, nil
		}
	}
	return nil, nil
}

// clause22Records собирает записи обязательных видов иной ОО: сколько
// мероприятий и на какую сумму подтверждено по Виду 1 и Виду 3.
func clause22Records(ctx context.Context, q workflowQuerier, agreementID string, year int, period string) ([]clause22Entry, bool, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.category_code,c.name,count(*),COALESCE(sum(e.amount_rub),0)
		FROM entries e JOIN activity_categories c ON c.code=e.category_code
		WHERE e.agreement_id::text=$1 AND e.report_year=$2 AND e.period_type=$3 AND e.amount_rub>0
		AND e.category_code IN ('teachers','ood_rpd')
		GROUP BY e.category_code,c.name ORDER BY c.name`, agreementID, year, period)
	if err != nil {
		return nil, false, false, err
	}
	defer rows.Close()
	records := []clause22Entry{}
	teachers, programs := false, false
	for rows.Next() {
		var record clause22Entry
		if err = rows.Scan(&record.CategoryCode, &record.CategoryName, &record.EntryCount, &record.AmountRub); err != nil {
			return nil, false, false, err
		}
		switch record.CategoryCode {
		case "teachers":
			teachers = true
		case "ood_rpd":
			programs = true
		}
		records = append(records, record)
	}
	if err = rows.Err(); err != nil {
		return nil, false, false, err
	}
	return records, teachers, programs, nil
}

// mandatoryHigherEducationActivities — обязательные виды мероприятий для
// соглашения с ОО высшего образования по ADR-11: Вид 1 (преподаватели) и
// Вид 3 (ООП/РПД). Для СПО и школ обязательных видов нет.
var mandatoryHigherEducationActivities = []string{"teachers", "ood_rpd"}

// clause22AlternativeSatisfied — условие п. 22 Порядка в трактовке ADR-03:
// ТОП-ИТ/ТОП-ИИ освобождает ОО «A» от прочих видов, если в иной ОО «B» в том
// же году одновременно реализованы обязательные Вид 1 (преподаватели) и Вид 3
// (ООП/РПД). Стажировки, практика и решения Минцифры — вариативные виды и в
// условие не входят: раньше их требование лишало компанию законного
// освобождения.
func clause22AlternativeSatisfied(teachers, programs bool) bool {
	return teachers && programs
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
	err = q.QueryRowContext(ctx, `SELECT status,scope_confirmed,conditions_confirmed,evidence_confirmed,counterparty_confirmed,actual_costs_confirmed,COALESCE(auditor_report_reference,''),comment,ready_at,verified_at,approved_at
		FROM agreement_reports WHERE agreement_id::text=$1 AND report_year=$2 AND period_type=$3`, agreementID, year, period).
		Scan(&resp.Status, &resp.ScopeConfirmed, &resp.ConditionsConfirmed, &resp.EvidenceConfirmed, &resp.CounterpartyConfirmed, &resp.ActualCostsConfirmed, &resp.AuditorReportReference, &comment, &readyAt, &verifiedAt, &approvedAt)
	if err != nil && err != sql.ErrNoRows {
		return resp, err
	}
	if comment.Valid {
		resp.Comment = comment.String
	}
	if readyAt.Valid {
		resp.ReadyAt = &readyAt.Time
		// Приказ № 270: 10 календарных дней на рассмотрение предварительного
		// перечня, 20 — итогового; счёт по датам по ADR-02.
		resp.ReviewDays = 10
		if period == "fact" {
			resp.ReviewDays = 20
		}
		lastDay, expiresAt := calendarDeadline(readyAt.Time, resp.ReviewDays)
		resp.ReviewDueDate = lastDay
		resp.ReviewDueAt = &expiresAt
		resp.ReviewOverdue = resp.Status == "ready" && deadlineExpired(expiresAt, time.Now())
	}
	if verifiedAt.Valid {
		resp.VerifiedAt = &verifiedAt.Time
	}
	if approvedAt.Valid {
		resp.ApprovedAt = &approvedAt.Time
	}

	// ADR-11 (OOP-04): для соглашения с ОО высшего образования Виды 1 и 3
	// обязательны сами по себе, даже если их забыли перечислить в составе
	// соглашения; для СПО и школ обязательности нет.
	if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agreement_partners ap JOIN partners p ON p.id=ap.partner_id
		WHERE ap.agreement_id::text=$1 AND p.partner_kind='vuz')`, agreementID).Scan(&resp.HigherEducation); err != nil {
		return resp, err
	}
	mandatory := []string{}
	if resp.HigherEducation {
		mandatory = mandatoryHigherEducationActivities
	}
	rows, err := q.QueryContext(ctx, `SELECT c.code,c.name,EXISTS(SELECT 1 FROM entries e WHERE e.agreement_id::text=$1
		AND e.report_year=$2 AND e.period_type=$3 AND e.category_code=c.code AND e.amount_rub>0)
		FROM activity_categories c
		WHERE c.code IN (SELECT category_code FROM agreement_activity_requirements WHERE agreement_id::text=$1)
		OR c.code = ANY($4) ORDER BY c.name`, agreementID, year, period, pq.Array(mandatory))
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
		basis, altErr := topITAlternativeBasis(ctx, q, agreementID, year, period)
		if altErr != nil {
			return resp, altErr
		}
		if basis != nil {
			activitiesComplete = true
			resp.TopITException = true
			resp.TopITBasis = basis
		}
	}
	yearStart := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
	agreementValid := agreementStatus == "active" && !from.After(yearEnd) && !until.Before(yearStart) && (kind != "roiv" || raStatus == "active")
	peopleValid := peopleCP > 0 && peopleOther > 0
	if err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE agreement_id::text=$1 AND report_year=$2 AND period_type=$3 AND cost_method='actual')`, agreementID, year, period).Scan(&resp.UsesActualCosts); err != nil {
		return resp, err
	}
	auditorValid := !resp.UsesActualCosts || (strings.TrimSpace(resp.AuditorReportReference) != "" && resp.ActualCostsConfirmed)
	resp.AutomaticChecks = []reportAutomaticCheck{
		{Code: "agreement", Label: "Соглашение действует в отчётном году и надлежаще подписано", Complete: agreementValid},
		{Code: "responsible_people", Label: "Указаны ответственные обеих сторон", Complete: peopleValid},
		{Code: "activity_scope", Label: "Перечень видов мероприятий задан в соглашении", Complete: len(required) > 0},
		{Code: "activities", Label: "По каждому виду есть мероприятие с положительной расчётной суммой", Complete: activitiesComplete},
	}
	if period == "fact" {
		entryRows, entryErr := q.QueryContext(ctx, `SELECT e.category_code,e.payload,
			ARRAY(SELECT DISTINCT a.document_type||':'||a.review_status FROM attachments a WHERE a.entry_id=e.id AND a.retention_expires_at>now())
			FROM entries e WHERE e.agreement_id::text=$1 AND e.report_year=$2 AND e.period_type=$3`, agreementID, year, period)
		if entryErr != nil {
			return resp, entryErr
		}
		total, incomplete := 0, 0
		for entryRows.Next() {
			var category string
			var payloadRaw []byte
			var documents pq.StringArray
			if entryErr = entryRows.Scan(&category, &payloadRaw, &documents); entryErr != nil {
				entryRows.Close()
				return resp, entryErr
			}
			payload := map[string]interface{}{}
			if entryErr = json.Unmarshal(payloadRaw, &payload); entryErr != nil {
				entryRows.Close()
				return resp, entryErr
			}
			total++
			if result := compliance.Evaluate(category, period, payload, []string(documents)); !result.Eligible {
				incomplete++
			}
		}
		if entryErr = entryRows.Err(); entryErr != nil {
			entryRows.Close()
			return resp, entryErr
		}
		entryRows.Close()
		resp.AutomaticChecks = append(resp.AutomaticChecks, reportAutomaticCheck{
			Code: "entry_readiness", Label: "Все мероприятия имеют зелёную документальную готовность",
			Complete: total > 0 && incomplete == 0, Detail: fmt.Sprintf("не готовы: %d из %d", incomplete, total),
		})
	}
	if period == "fact" {
		resp.AutomaticChecks = append(resp.AutomaticChecks, reportAutomaticCheck{Code: "actual_costs", Label: "Для фактических затрат указано аудиторское заключение", Complete: auditorValid})
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
	// Counterparty review happens only after the IT organization has prepared
	// and submitted the fact report. It therefore cannot be a prerequisite for
	// moving the organization's draft to ready.
	manualComplete := resp.ScopeConfirmed && resp.ConditionsConfirmed && resp.EvidenceConfirmed
	if !resp.ScopeConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждено соответствие конкретному перечню, объёму, срокам и условиям соглашения")
	}
	if !resp.ConditionsConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждены условия реализации видов мероприятий по приложению № 1")
	}
	if !resp.EvidenceConfirmed {
		resp.Missing = append(resp.Missing, "Не подтверждено наличие однозначных подтверждающих документов")
	}
	if period == "fact" && resp.Status != "draft" && !resp.CounterpartyConfirmed {
		resp.Missing = append(resp.Missing, "Ожидается рассмотрение перечня образовательной организацией или РОИВ")
	}
	automaticComplete := true
	for _, check := range resp.AutomaticChecks {
		automaticComplete = automaticComplete && check.Complete
	}
	resp.CanMarkReady = resp.Status == "draft" && automaticComplete && manualComplete && canPrepareReports(u)
	reviewerAllowed := canReviewReport(u, period)
	if reviewerAllowed && period == "fact" && isEducationRepresentative(u) {
		var partnerCount int
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM agreement_partners WHERE agreement_id::text=$1`, agreementID).Scan(&partnerCount); err != nil {
			return resp, err
		}
		// One representative must not accept a combined list on behalf of the
		// other educational organizations covered by the same agreement.
		reviewerAllowed = partnerCount == 1
	}
	resp.CanVerify = resp.Status == "ready" && reviewerAllowed
	resp.CanApprove = resp.Status == "verified" && canApproveReports(u)
	resp.CanReturnDraft = resp.Status != "draft" && (canPrepareReports(u) || (period == "fact" && resp.Status == "ready" && reviewerAllowed && isEducationRepresentative(u)))
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
	Status                 string `json:"status"`
	ScopeConfirmed         bool   `json:"scope_confirmed"`
	ConditionsConfirmed    bool   `json:"conditions_confirmed"`
	EvidenceConfirmed      bool   `json:"evidence_confirmed"`
	ActualCostsConfirmed   bool   `json:"actual_costs_confirmed"`
	AuditorReportReference string `json:"auditor_report_reference"`
	CounterpartyConfirmed  bool   `json:"counterparty_confirmed"`
	Comment                string `json:"comment"`
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
	req.AuditorReportReference = strings.TrimSpace(req.AuditorReportReference)
	if len([]rune(req.Comment)) > 1000 {
		middleware.WriteError(w, 400, "комментарий не должен превышать 1000 символов")
		return
	}
	if len([]rune(req.AuditorReportReference)) > 1000 {
		middleware.WriteError(w, 400, "реквизиты аудиторского заключения не должны превышать 1000 символов")
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
	if isEducationRepresentative(u) {
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
		allowed = current == "draft" && canPrepareReports(u)
	case "verified":
		allowed = current == "ready" && canReviewReport(u, period)
	case "approved":
		allowed = current == "verified" && canApproveReports(u)
	case "draft":
		allowed = current != "draft" && (canPrepareReports(u) || (period == "fact" && current == "ready" && isEducationRepresentative(u)))
	}
	if !allowed {
		middleware.WriteError(w, 409, "недопустимый переход статуса или недостаточно прав")
		return
	}
	if req.Status == "ready" {
		_, err = tx.ExecContext(r.Context(), `UPDATE agreement_reports SET scope_confirmed=$4,conditions_confirmed=$5,evidence_confirmed=$6,actual_costs_confirmed=$7,auditor_report_reference=NULLIF($8,''),counterparty_confirmed=FALSE,comment=NULLIF($9,''),updated_by=$10,updated_at=now() WHERE agreement_id=$1 AND report_year=$2 AND period_type=$3`, id, year, period, req.ScopeConfirmed, req.ConditionsConfirmed, req.EvidenceConfirmed, req.ActualCostsConfirmed, req.AuditorReportReference, req.Comment, u.ID)
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
		if req.Status == "verified" && period == "fact" {
			// A regular educational representative records actual acceptance;
			// an administrator/moderator may record deemed acceptance after the
			// response period. In both cases the actor and basis remain in history.
			if _, err = tx.ExecContext(r.Context(), `UPDATE agreement_reports SET counterparty_confirmed=TRUE,updated_by=$4,updated_at=now() WHERE agreement_id=$1 AND report_year=$2 AND period_type=$3`, id, year, period, u.ID); err != nil {
				middleware.WriteError(w, 500, "ошибка сохранения рассмотрения контрагентом")
				return
			}
		}
		check, checkErr := buildWorkflow(r.Context(), tx, u, id, year, period)
		if checkErr != nil {
			middleware.WriteError(w, 500, "ошибка повторной проверки отчёта")
			return
		}
		manualOK := check.ScopeConfirmed && check.ConditionsConfirmed && check.EvidenceConfirmed && (!check.UsesActualCosts || (check.ActualCostsConfirmed && check.AuditorReportReference != "")) && (period == "plan" || check.CounterpartyConfirmed)
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
		stamp += ",scope_confirmed=FALSE,conditions_confirmed=FALSE,evidence_confirmed=FALSE,actual_costs_confirmed=FALSE,auditor_report_reference=NULL,counterparty_confirmed=FALSE,ready_by=NULL,ready_at=NULL,verified_by=NULL,verified_at=NULL,approved_by=NULL,approved_at=NULL"
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
	if err = logAudit(r.Context(), tx, "report", id, "status", u.ID, req.Comment, map[string]string{"status": current}, map[string]string{"status": req.Status}); err != nil {
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
