package handlers

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"strings"
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
	h.DB.QueryRow(`SELECT target_amount_rub FROM budget_targets WHERE report_year = $1 AND owner_user_id = $2`, year, u.ID).Scan(&target)
	if target.Valid {
		resp.TargetAmountRub = &target.Float64
	}

	resp.PlanTotalRub = h.total(year, "plan", u)
	resp.FactTotalRub = h.total(year, "fact", u)

	if resp.PlanTotalRub > 0 {
		resp.PlanCompletionPct = round2(resp.FactTotalRub / resp.PlanTotalRub * 100)
	}

	resp.PlanByCategory = h.breakdown(year, "plan", u)
	resp.FactByCategory = h.breakdown(year, "fact", u)

	middleware.WriteJSON(w, http.StatusOK, resp)
}

func dashboardConditions(year int, period string, u middleware.AuthUser) ([]string, []interface{}) {
	conditions := []string{"period_type = $1", "report_year = $2"}
	args := []interface{}{period, year}
	return appendEntryScope(conditions, args, u, "")
}

func (h *DashboardHandlers) total(year int, period string, u middleware.AuthUser) float64 {
	conditions, args := dashboardConditions(year, period, u)
	var total float64
	_ = h.DB.QueryRow(`SELECT COALESCE(SUM(amount_rub),0) FROM entries WHERE `+strings.Join(conditions, " AND "), args...).Scan(&total)
	return total
}

func (h *DashboardHandlers) breakdown(year int, period string, u middleware.AuthUser) []categoryBreakdown {
	conditions, args := dashboardConditions(year, period, u)
	rows, err := h.DB.Query(
		`SELECT category_code, COALESCE(SUM(amount_rub),0) FROM entries WHERE `+strings.Join(conditions, " AND ")+
			` GROUP BY category_code ORDER BY category_code`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var total float64
	raw := make([]categoryBreakdown, 0)
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
