package handlers

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
)

type DashboardHandlers struct {
	DB *sql.DB
}

type categoryBreakdown struct {
	CategoryCode string       `json:"category_code"`
	AmountRub    money.Amount `json:"amount_rub"`
	SharePercent float64      `json:"share_percent"`
}

type dashboardResponse struct {
	EligiblePlanTotalRub money.Amount        `json:"eligible_plan_total_rub"`
	EligibleFactTotalRub money.Amount        `json:"eligible_fact_total_rub"`
	IncompleteEntries    int                 `json:"incomplete_entries"`
	ReportYear           int                 `json:"report_year"`
	TargetAmountRub      *money.Amount       `json:"target_amount_rub"` // "Общие затраты (3% от сэкономленных льгот)"
	PlanTotalRub         money.Amount        `json:"plan_total_rub"`
	FactTotalRub         money.Amount        `json:"fact_total_rub"`
	PlanCompletionPct    float64             `json:"plan_completion_pct"` // % реализации плана
	PlanByCategory       []categoryBreakdown `json:"plan_by_category"`
	FactByCategory       []categoryBreakdown `json:"fact_by_category"`
}

func (h *DashboardHandlers) Get(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	year := time.Now().Year()
	if y := r.URL.Query().Get("report_year"); y != "" {
		if parsed, err := strconv.Atoi(y); err == nil {
			year = parsed
		} else {
			middleware.WriteError(w, 400, "некорректный год")
			return
		}
	}

	resp := dashboardResponse{ReportYear: year}
	if year < 2000 || year > 2100 {
		middleware.WriteError(w, 400, "некорректный год")
		return
	}
	scope := partnerScope(u, r.URL.Query().Get("partner_id"))

	var target money.Amount
	targetErr := h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub FROM organization_budget_targets WHERE report_year = $1`, year).Scan(&target)
	if targetErr != nil && targetErr != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка чтения целевой суммы")
		return
	}
	if targetErr == nil && isStaff(u) && scope == "" {
		resp.TargetAmountRub = &target
	}

	if err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(amount_rub) FILTER(WHERE period_type='plan'),0),COALESCE(SUM(amount_rub) FILTER(WHERE period_type='fact'),0) FROM entries WHERE report_year=$1 AND ($2='' OR partner_id::text=$2)`, year, scope).Scan(&resp.PlanTotalRub, &resp.FactTotalRub); err != nil {
		middleware.WriteError(w, 500, "ошибка расчёта дашборда")
		return
	}

	if resp.PlanTotalRub > 0 {
		resp.PlanCompletionPct = round2(resp.FactTotalRub.Rubles() / resp.PlanTotalRub.Rubles() * 100)
	}
	if err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(sum(amount_rub) FILTER(WHERE period_type='plan' AND eligible),0),COALESCE(sum(amount_rub) FILTER(WHERE period_type='fact' AND eligible),0),count(*) FILTER(WHERE NOT eligible) FROM entry_eligibility WHERE report_year=$1 AND ($2='' OR partner_id::text=$2)`, year, scope).Scan(&resp.EligiblePlanTotalRub, &resp.EligibleFactTotalRub, &resp.IncompleteEntries); err != nil {
		middleware.WriteError(w, 500, "ошибка проверки обязательностей")
		return
	}

	var err error
	resp.PlanByCategory, err = h.breakdown(r, year, "plan", scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики плана")
		return
	}
	resp.FactByCategory, err = h.breakdown(r, year, "fact", scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики факта")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *DashboardHandlers) breakdown(r *http.Request, year int, period, scope string) ([]categoryBreakdown, error) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT category_code, COALESCE(SUM(amount_rub),0) FROM entries
		 WHERE period_type = $1 AND report_year = $2 AND ($3='' OR partner_id::text=$3)
		 GROUP BY category_code ORDER BY category_code`, period, year, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var total money.Amount
	raw := make([]categoryBreakdown, 0)
	for rows.Next() {
		var b categoryBreakdown
		if err := rows.Scan(&b.CategoryCode, &b.AmountRub); err != nil {
			return nil, err
		}
		var sumErr error
		total, sumErr = money.Add(total, b.AmountRub)
		if sumErr != nil {
			return nil, sumErr
		}
		raw = append(raw, b)
	}
	if total > 0 {
		for i := range raw {
			raw[i].SharePercent = round2(raw[i].AmountRub.Rubles() / total.Rubles() * 100)
		}
	}
	return raw, rows.Err()
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

type setBudgetTargetRequest struct {
	ReportYear      int          `json:"report_year"`
	TargetAmountRub money.Amount `json:"target_amount_rub"`
}

func (h *DashboardHandlers) SetBudgetTarget(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "целевую сумму задаёт сотрудник Киберпротекта")
		return
	}
	var req setBudgetTargetRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.ReportYear < 2000 || req.ReportYear > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "report_year должен быть в диапазоне 2000–2100")
		return
	}
	if req.TargetAmountRub <= 0 || req.TargetAmountRub > 9_999_999_999_999_999 {
		middleware.WriteError(w, http.StatusBadRequest, "целевая сумма должна быть положительным числом в допустимом диапазоне")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(),
		`INSERT INTO organization_budget_targets (report_year, target_amount_rub, updated_by)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (report_year) DO UPDATE SET target_amount_rub = $2, updated_by = $3, updated_at = now()`,
		req.ReportYear, req.TargetAmountRub, u.ID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	if logAudit(tx, "settings", "", "update", u.ID, "изменение целевой суммы (3%)", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
