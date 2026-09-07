package handlers

import (
	"database/sql"
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
		}
	}

	resp := dashboardResponse{ReportYear: year}

	var target sql.NullFloat64
	h.DB.QueryRow(`SELECT target_amount_rub FROM budget_targets WHERE report_year = $1`, year).Scan(&target)
	if target.Valid {
		resp.TargetAmountRub = &target.Float64
	}

	h.DB.QueryRow(`SELECT COALESCE(SUM(amount_rub),0) FROM entries WHERE period_type='plan' AND report_year=$1`, year).
		Scan(&resp.PlanTotalRub)
	h.DB.QueryRow(`SELECT COALESCE(SUM(amount_rub),0) FROM entries WHERE period_type='fact' AND report_year=$1`, year).
		Scan(&resp.FactTotalRub)

	if resp.PlanTotalRub > 0 {
		resp.PlanCompletionPct = round2(resp.FactTotalRub / resp.PlanTotalRub * 100)
	}

	resp.PlanByCategory = h.breakdown(year, "plan")
	resp.FactByCategory = h.breakdown(year, "fact")

	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *DashboardHandlers) breakdown(year int, period string) []categoryBreakdown {
	rows, err := h.DB.Query(
		`SELECT category_code, COALESCE(SUM(amount_rub),0) FROM entries
		 WHERE period_type = $1 AND report_year = $2
		 GROUP BY category_code ORDER BY category_code`, period, year)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var total float64
	var raw []categoryBreakdown
	for rows.Next() {
		var b categoryBreakdown
		if err := rows.Scan(&b.CategoryCode, &b.AmountRub); err != nil {
			continue
		}
		total += b.AmountRub
		raw = append(raw, b)
	}
	if total > 0 {
		for i := range raw {
			raw[i].SharePercent = round2(raw[i].AmountRub / total * 100)
		}
	}
	return raw
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

type setBudgetTargetRequest struct {
	ReportYear      int     `json:"report_year"`
	TargetAmountRub float64 `json:"target_amount_rub"`
}

func (h *DashboardHandlers) SetBudgetTarget(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req setBudgetTargetRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	_, err := h.DB.Exec(
		`INSERT INTO budget_targets (report_year, target_amount_rub, updated_by)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (report_year) DO UPDATE SET target_amount_rub = $2, updated_by = $3, updated_at = now()`,
		req.ReportYear, req.TargetAmountRub, u.ID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	logAudit(h.DB, "settings", "", "update", u.ID, "изменение целевой суммы (3%)", nil, req)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
