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
	Audience     string       `json:"audience"`
	Obligation   string       `json:"obligation"`
	EntryCount   int          `json:"entry_count"`
	UnitCount    float64      `json:"unit_count"`
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
	CategoryFilter       string              `json:"category_filter,omitempty"`
	AudienceFilter       string              `json:"audience_filter,omitempty"`
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
	companyScope := itCompanyScope(u)
	categoryFilter := r.URL.Query().Get("category_code")
	audienceFilter := r.URL.Query().Get("audience")
	if audienceFilter != "" && audienceFilter != "vuz" && audienceFilter != "kolledj" && audienceFilter != "school" {
		middleware.WriteError(w, 400, "некорректная аудитория")
		return
	}
	if categoryFilter != "" {
		var exists bool
		if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM activity_categories WHERE code=$1)`, categoryFilter).Scan(&exists); err != nil || !exists {
			middleware.WriteError(w, 400, "некорректный вид активности")
			return
		}
	}
	resp.CategoryFilter, resp.AudienceFilter = categoryFilter, audienceFilter
	if companyScope == "" && scope != "" && scope != "unassigned" {
		_ = h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(it_company_id::text,'') FROM partners WHERE id::text=$1`, scope).Scan(&companyScope)
	}

	var target money.Amount
	targetErr := sql.ErrNoRows
	if companyScope != "" {
		targetErr = h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub FROM organization_budget_targets WHERE report_year=$1 AND it_company_id::text=$2`, year, companyScope).Scan(&target)
	}
	if targetErr != nil && targetErr != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка чтения целевой суммы")
		return
	}
	if targetErr == nil && isStaff(u) && scope == "" {
		resp.TargetAmountRub = &target
	}

	if err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(amount_rub) FILTER(WHERE period_type='plan'),0),COALESCE(SUM(amount_rub) FILTER(WHERE period_type='fact'),0) FROM entries WHERE report_year=$1 AND ($2='' OR partner_id::text=$2) AND ($3='' OR it_company_id::text=$3) AND ($4='' OR category_code=$4) AND ($5='' OR audience=$5)`, year, scope, companyScope, categoryFilter, audienceFilter).Scan(&resp.PlanTotalRub, &resp.FactTotalRub); err != nil {
		middleware.WriteError(w, 500, "ошибка расчёта дашборда")
		return
	}

	if resp.PlanTotalRub > 0 {
		resp.PlanCompletionPct = round2(resp.FactTotalRub.Rubles() / resp.PlanTotalRub.Rubles() * 100)
	}
	if err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(sum(e.amount_rub) FILTER(WHERE e.period_type='plan' AND eligibility.eligible),0),COALESCE(sum(e.amount_rub) FILTER(WHERE e.period_type='fact' AND eligibility.eligible),0),count(*) FILTER(WHERE NOT eligibility.eligible) FROM entry_eligibility eligibility JOIN entries e ON e.id=eligibility.id WHERE e.report_year=$1 AND ($2='' OR e.partner_id::text=$2) AND ($3='' OR e.it_company_id::text=$3) AND ($4='' OR e.category_code=$4) AND ($5='' OR e.audience=$5)`, year, scope, companyScope, categoryFilter, audienceFilter).Scan(&resp.EligiblePlanTotalRub, &resp.EligibleFactTotalRub, &resp.IncompleteEntries); err != nil {
		middleware.WriteError(w, 500, "ошибка проверки обязательностей")
		return
	}

	var err error
	resp.PlanByCategory, err = h.breakdown(r, year, "plan", scope, companyScope, categoryFilter, audienceFilter)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики плана")
		return
	}
	resp.FactByCategory, err = h.breakdown(r, year, "fact", scope, companyScope, categoryFilter, audienceFilter)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка аналитики факта")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, resp)
}

func (h *DashboardHandlers) breakdown(r *http.Request, year int, period, scope, companyScope, categoryFilter, audienceFilter string) ([]categoryBreakdown, error) {
	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT e.category_code,e.audience,c.obligation,count(*),
		 COALESCE(sum(CASE
		   WHEN e.category_code='teacher_training' THEN COALESCE((e.payload->>'trained_teachers_count')::numeric,0)
		   WHEN e.category_code='it_clubs' THEN COALESCE((e.payload->>'developed_programs_count')::numeric,0)
		   WHEN e.category_code='edu_content' THEN COALESCE((e.payload->>'student_platform_months')::numeric,0)+COALESCE((e.payload->>'teacher_platform_months')::numeric,0)
		   ELSE 1 END),0)::float8,
		 COALESCE(SUM(e.amount_rub),0)
		 FROM entries e JOIN activity_categories c ON c.code=e.category_code
		 WHERE e.period_type=$1 AND e.report_year=$2 AND ($3='' OR e.partner_id::text=$3)
		 AND ($4='' OR e.it_company_id::text=$4) AND ($5='' OR e.category_code=$5) AND ($6='' OR e.audience=$6)
		 GROUP BY e.category_code,e.audience,c.obligation ORDER BY e.category_code,e.audience`, period, year, scope, companyScope, categoryFilter, audienceFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var total money.Amount
	raw := make([]categoryBreakdown, 0)
	for rows.Next() {
		var b categoryBreakdown
		if err := rows.Scan(&b.CategoryCode, &b.Audience, &b.Obligation, &b.EntryCount, &b.UnitCount, &b.AmountRub); err != nil {
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
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
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
		`INSERT INTO organization_budget_targets (report_year, it_company_id,target_amount_rub, updated_by)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (report_year,it_company_id) DO UPDATE SET target_amount_rub = $3, updated_by = $4, updated_at = now()`,
		req.ReportYear, companyID, req.TargetAmountRub, u.ID,
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
