package handlers

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"time"

	"cybercalc/internal/middleware"
)

type DashboardHandlers struct {
	DB *sql.DB
}

type categoryBreakdown struct {
	CategoryCode string  `json:"category_code"`
	AmountRub    float64 `json:"amount_rub"`
	SharePercent float64 `json:"share_percent"`
}

type dashboardResponse struct {
	ReportYear        int                 `json:"report_year"`
	TargetAmountRub   *float64            `json:"target_amount_rub"` // "Общие затраты (3% от сэкономленных льгот)"
	PlanTotalRub      float64             `json:"plan_total_rub"`
	FactTotalRub      float64             `json:"fact_total_rub"`
	PlanCompletionPct float64             `json:"plan_completion_pct"` // % реализации плана
	PlanByCategory    []categoryBreakdown `json:"plan_by_category"`
	FactByCategory    []categoryBreakdown `json:"fact_by_category"`
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

	var target sql.NullFloat64
	if err := h.DB.QueryRow(`SELECT target_amount_rub FROM budget_targets WHERE report_year = $1 AND owner_user_id = $2`, year, u.ID).Scan(&target); err != nil && err != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка чтения целевой суммы")
		return
	}
	if target.Valid && isStaff(u) && scope == "" {
		resp.TargetAmountRub = &target.Float64
	}

	if err := h.DB.QueryRow(`SELECT COALESCE(SUM(amount_rub) FILTER(WHERE period_type='plan'),0),COALESCE(SUM(amount_rub) FILTER(WHERE period_type='fact'),0) FROM entries WHERE report_year=$1 AND ($2='' OR partner_id::text=$2)`, year, scope).Scan(&resp.PlanTotalRub, &resp.FactTotalRub); err != nil {
		middleware.WriteError(w, 500, "ошибка расчёта дашборда")
		return
	}

	if resp.PlanTotalRub > 0 {
		resp.PlanCompletionPct = round2(resp.FactTotalRub / resp.PlanTotalRub * 100)
	}

	var err error
	resp.PlanByCategory, err = h.breakdown(year, "plan", scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики плана")
		return
	}
	resp.FactByCategory, err = h.breakdown(year, "fact", scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики факта")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *DashboardHandlers) breakdown(year int, period, scope string) ([]categoryBreakdown, error) {
	rows, err := h.DB.Query(
		`SELECT category_code, COALESCE(SUM(amount_rub),0) FROM entries
		 WHERE period_type = $1 AND report_year = $2 AND ($3='' OR partner_id::text=$3)
		 GROUP BY category_code ORDER BY category_code`, period, year, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var total float64
	raw := make([]categoryBreakdown, 0)
	for rows.Next() {
		var b categoryBreakdown
		if err := rows.Scan(&b.CategoryCode, &b.AmountRub); err != nil {
			return nil, err
		}
		total += b.AmountRub
		raw = append(raw, b)
	}
	if total > 0 {
		for i := range raw {
			raw[i].SharePercent = round2(raw[i].AmountRub / total * 100)
		}
	}
	return raw, rows.Err()
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

type setBudgetTargetRequest struct {
	ReportYear      int     `json:"report_year"`
	TargetAmountRub float64 `json:"target_amount_rub"`
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
	if math.IsNaN(req.TargetAmountRub) || math.IsInf(req.TargetAmountRub, 0) || req.TargetAmountRub <= 0 || req.TargetAmountRub > 99_999_999_999_999.99 {
		middleware.WriteError(w, http.StatusBadRequest, "целевая сумма должна быть положительным числом в допустимом диапазоне")
		return
	}
	_, err := h.DB.Exec(
		`INSERT INTO budget_targets (report_year, owner_user_id, target_amount_rub, updated_by)
		 VALUES ($1,$3,$2,$3)
		 ON CONFLICT (report_year, owner_user_id) DO UPDATE SET target_amount_rub = $2, updated_by = $3, updated_at = now()`,
		req.ReportYear, req.TargetAmountRub, u.ID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	logAudit(h.DB, "settings", "", "update", u.ID, "изменение целевой суммы (3%)", nil, req)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
