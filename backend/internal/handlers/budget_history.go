package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
)

// budgetTargetRevision — одна редакция норматива 3% со своим основанием.
// Норматив без основания непроверяем, поэтому база, её год, дата доведения и
// подтверждение ФНС хранятся и отдаются вместе с суммой.
type budgetTargetRevision struct {
	ReportYear      int           `json:"report_year"`
	BasisYear       *int          `json:"basis_year"`
	TargetAmountRub money.Amount  `json:"target_amount_rub"`
	SavingsBaseRub  *money.Amount `json:"savings_base_rub,omitempty"`
	SourceReference string        `json:"source_reference,omitempty"`
	NotifiedAt      string        `json:"notified_at,omitempty"`
	FNSConfirmedAt  string        `json:"fns_confirmed_at,omitempty"`
	FNSReference    string        `json:"fns_reference,omitempty"`
	Operation       string        `json:"operation"`
	ChangedBy       string        `json:"changed_by,omitempty"`
	ChangedByName   string        `json:"changed_by_name,omitempty"`
	ChangedAt       string        `json:"changed_at"`
}

// DATA-11: история изменений норматива. Пересчёт задним числом без следа —
// это спор, который нечем закрыть, поэтому каждая редакция сохраняется и
// доступна для чтения.
func (h *DashboardHandlers) BudgetTargetHistory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !isStaff(u) {
		middleware.WriteError(w, http.StatusForbidden, "историю норматива смотрит сотрудник ИТ-организации")
		return
	}
	company := itCompanyScope(u)
	if company == "" || company == "unassigned" {
		middleware.WriteError(w, http.StatusForbidden, "для операции назначьте профилю действующую ИТ-компанию")
		return
	}

	args := []interface{}{company}
	yearFilter := ""
	if value := r.URL.Query().Get("report_year"); value != "" {
		year, err := strconv.Atoi(value)
		if err != nil || year < 2000 || year > 2100 {
			middleware.WriteError(w, http.StatusBadRequest, "некорректный год")
			return
		}
		args = append(args, year)
		yearFilter = " AND h.report_year=$2"
	}
	// Структура запроса постоянна, отличается только наличием фильтра по году.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT h.report_year,h.basis_year,h.target_amount_rub,h.savings_base_rub,
		COALESCE(h.source_reference,''),COALESCE(h.notified_at::text,''),
		COALESCE(h.fns_confirmed_at::text,''),COALESCE(h.fns_reference,''),
		h.operation,COALESCE(h.changed_by::text,''),COALESCE(u.full_name,''),h.changed_at::text
		FROM organization_budget_target_history h
		LEFT JOIN users u ON u.id=h.changed_by
		WHERE h.it_company_id::text=$1` + yearFilter + `
		ORDER BY h.changed_at DESC,h.id DESC LIMIT 200`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории норматива")
		return
	}
	defer rows.Close()

	out := make([]budgetTargetRevision, 0)
	for rows.Next() {
		var item budgetTargetRevision
		var basisYear sql.NullInt64
		var savings sql.NullString
		if err := rows.Scan(&item.ReportYear, &basisYear, &item.TargetAmountRub, &savings,
			&item.SourceReference, &item.NotifiedAt, &item.FNSConfirmedAt, &item.FNSReference,
			&item.Operation, &item.ChangedBy, &item.ChangedByName, &item.ChangedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории норматива")
			return
		}
		if basisYear.Valid {
			year := int(basisYear.Int64)
			item.BasisYear = &year
		}
		if savings.Valid {
			if parsed, parseErr := money.Parse(savings.String); parseErr == nil {
				item.SavingsBaseRub = &parsed
			}
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения истории норматива")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}
