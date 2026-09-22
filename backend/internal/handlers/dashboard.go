package handlers

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"github.com/lib/pq"
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
	EligiblePlanTotalRub money.Amount          `json:"eligible_plan_total_rub"`
	EligibleFactTotalRub money.Amount          `json:"eligible_fact_total_rub"`
	IncompleteEntries    int                   `json:"incomplete_entries"`
	ReportYear           int                   `json:"report_year"`
	GeneratedAt          time.Time             `json:"generated_at"`
	TargetAmountRub      *money.Amount         `json:"target_amount_rub"` // "Общие затраты (3% от сэкономленных льгот)"
	SavingsBaseRub       *money.Amount         `json:"savings_base_rub,omitempty"`
	TargetSource         string                `json:"target_source_reference,omitempty"`
	TargetNotifiedAt     string                `json:"target_notified_at,omitempty"`
	TargetConfirmedRub   *money.Amount         `json:"target_confirmed_fact_rub"`
	PlanTotalRub         money.Amount          `json:"plan_total_rub"`
	FactTotalRub         money.Amount          `json:"fact_total_rub"`
	PlanCompletionPct    float64               `json:"plan_completion_pct"` // % реализации плана
	PlanByCategory       []categoryBreakdown   `json:"plan_by_category"`
	FactByCategory       []categoryBreakdown   `json:"fact_by_category"`
	CategoryFilter       string                `json:"category_filter,omitempty"`
	AudienceFilter       string                `json:"audience_filter,omitempty"`
	RiskBuckets          map[string]riskBucket `json:"risk_buckets"`
}

type riskBucket struct {
	EntryCount int          `json:"entry_count"`
	AmountRub  money.Amount `json:"amount_rub"`
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

	resp := dashboardResponse{ReportYear: year, GeneratedAt: time.Now().UTC()}
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

	var target, savingsBase money.Amount
	var savingsBaseRaw, source, notified sql.NullString
	targetErr := sql.ErrNoRows
	if companyScope != "" {
		targetErr = h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub,savings_base_rub::text,source_reference,notified_at::text FROM organization_budget_targets WHERE report_year=$1 AND it_company_id::text=$2`, year, companyScope).Scan(&target, &savingsBaseRaw, &source, &notified)
	}
	if targetErr != nil && targetErr != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка чтения целевой суммы")
		return
	}
	if targetErr == nil && isStaff(u) && scope == "" {
		resp.TargetAmountRub = &target
		if savingsBaseRaw.Valid {
			if parsed, parseErr := money.Parse(savingsBaseRaw.String); parseErr == nil {
				savingsBase = parsed
				resp.SavingsBaseRub = &savingsBase
			}
		}
		if source.Valid {
			resp.TargetSource = source.String
		}
		if notified.Valid {
			resp.TargetNotifiedAt = notified.String
		}
		companyRisk, riskErr := h.riskBreakdown(r, year, "", companyScope, "", "")
		if riskErr != nil {
			middleware.WriteError(w, 500, "ошибка расчёта подтверждённых расходов")
			return
		}
		confirmed := companyRisk["green"].AmountRub
		resp.TargetConfirmedRub = &confirmed
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
	resp.RiskBuckets, err = h.riskBreakdown(r, year, scope, companyScope, categoryFilter, audienceFilter)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка расчёта документальных рисков")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, resp)
}

// riskBucketState раскладывает запись ровно по одной корзине дашборда
// (DASH-02): гарантированный факт, прогноз и объём риска не пересекаются и в
// сумме дают весь факт. Полный комплект без формального утверждения — это
// прогноз, а не гарантия. Незнакомое состояние движка считается риском: в
// гарантированный объём попадает только то, что доказано.
func riskBucketState(state string, approved bool) string {
	switch state {
	case "green":
		if !approved {
			return "yellow"
		}
		return "green"
	case "yellow", "red":
		return state
	default:
		return "red"
	}
}

func (h *DashboardHandlers) riskBreakdown(r *http.Request, year int, scope, companyScope, categoryFilter, audienceFilter string) (map[string]riskBucket, error) {
	result := map[string]riskBucket{"green": {}, "yellow": {}, "red": {}}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT e.category_code,e.payload,e.amount_rub,eligibility.eligible,
		ARRAY(SELECT DISTINCT a.document_type||':'||a.review_status FROM attachments a WHERE a.entry_id=e.id AND a.retention_expires_at>now())
		FROM entries e JOIN entry_eligibility eligibility ON eligibility.id=e.id
		WHERE e.period_type='fact' AND e.report_year=$1 AND ($2='' OR e.partner_id::text=$2)
		AND ($3='' OR e.it_company_id::text=$3) AND ($4='' OR e.category_code=$4) AND ($5='' OR e.audience=$5)`, year, scope, companyScope, categoryFilter, audienceFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var category string
		var payloadRaw []byte
		var amount money.Amount
		var approved bool
		var documents pq.StringArray
		if err := rows.Scan(&category, &payloadRaw, &amount, &approved, &documents); err != nil {
			return nil, err
		}
		payload := map[string]interface{}{}
		if err := json.Unmarshal(payloadRaw, &payload); err != nil {
			return nil, err
		}
		state := riskBucketState(compliance.Evaluate(category, "fact", payload, []string(documents)).State, approved)
		bucket := result[state]
		bucket.EntryCount++
		var addErr error
		bucket.AmountRub, addErr = money.Add(bucket.AmountRub, amount)
		if addErr != nil {
			return nil, addErr
		}
		result[state] = bucket
	}
	return result, rows.Err()
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
	ReportYear      int           `json:"report_year"`
	TargetAmountRub money.Amount  `json:"target_amount_rub"`
	SavingsBaseRub  *money.Amount `json:"savings_base_rub,omitempty"`
	SourceReference string        `json:"source_reference"`
	NotifiedAt      string        `json:"notified_at"`
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
	req.SourceReference = strings.TrimSpace(req.SourceReference)
	if req.SavingsBaseRub == nil || *req.SavingsBaseRub <= 0 || req.SourceReference == "" || req.NotifiedAt == "" {
		middleware.WriteError(w, 400, "укажите положительную базу экономии N-2, источник и дату доведения Минцифры")
		return
	}
	if len([]rune(req.SourceReference)) > 2000 {
		middleware.WriteError(w, 400, "источник базы не должен превышать 2000 символов")
		return
	}
	if _, err := time.Parse("2006-01-02", req.NotifiedAt); err != nil || req.NotifiedAt > time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01-02") {
		middleware.WriteError(w, 400, "дата доведения Минцифры должна быть корректной и не из будущего")
		return
	}
	if int64(*req.SavingsBaseRub) > (math.MaxInt64-50)/3 {
		middleware.WriteError(w, 400, "база экономии превышает допустимый предел")
		return
	}
	expected := money.Amount((int64(*req.SavingsBaseRub)*3 + 50) / 100)
	if req.TargetAmountRub != expected {
		middleware.WriteError(w, 400, "минимальный объём должен составлять ровно 3% от указанной базы экономии")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сервера")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(),
		`INSERT INTO organization_budget_targets (report_year,it_company_id,target_amount_rub,savings_base_rub,source_reference,notified_at,updated_by)
		 VALUES ($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::date,$7)
		 ON CONFLICT (report_year,it_company_id) DO UPDATE SET target_amount_rub=$3,savings_base_rub=$4,source_reference=NULLIF($5,''),notified_at=NULLIF($6,'')::date,updated_by=$7,updated_at=now()`,
		req.ReportYear, companyID, req.TargetAmountRub, req.SavingsBaseRub, req.SourceReference, req.NotifiedAt, u.ID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	if logAudit(r.Context(), tx, "settings", "", "update", u.ID, "изменение целевой суммы (3%)", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
