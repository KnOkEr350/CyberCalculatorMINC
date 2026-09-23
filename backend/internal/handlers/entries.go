package handlers

import (
	"cybercalc/internal/compliance"
	"cybercalc/internal/money"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"cybercalc/internal/calculators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type EntryHandlers struct {
	DB *sql.DB
}

func appendEntryDetailFilters(q url.Values, conds *[]string, arg func(interface{}) string) {
	if value := strings.TrimSpace(q.Get("q")); value != "" {
		*conds = append(*conds, "payload::text ILIKE '%'||"+arg(value)+"||'%'")
	}
	likeFields := map[string]string{
		"teacher_full_name":  "teacher_full_name",
		"teaching_area":      "teaching_area",
		"training_direction": "training_direction",
		"mentor_name":        "mentor_full_name",
		"structural_unit":    "department",
		"cost_type":          "cost_type",
	}
	for parameter, field := range likeFields {
		if value := strings.TrimSpace(q.Get(parameter)); value != "" {
			if parameter == "structural_unit" {
				placeholder := arg(value)
				*conds = append(*conds, "(payload->>'department' ILIKE '%'||"+placeholder+"||'%' OR payload->>'institute' ILIKE '%'||"+placeholder+"||'%' OR payload->>'faculty' ILIKE '%'||"+placeholder+"||'%')")
			} else {
				*conds = append(*conds, "payload->>'"+field+"' ILIKE '%'||"+arg(value)+"||'%'")
			}
		}
	}
	exactFields := map[string]string{"doc_type": "doc_type", "activity_type": "activity_type", "mentor_id": "mentor_id", "cost_method": "cost_method"}
	for parameter, field := range exactFields {
		if value := strings.TrimSpace(q.Get(parameter)); value != "" {
			if parameter == "cost_method" {
				*conds = append(*conds, "cost_method="+arg(value))
			} else if parameter == "activity_type" {
				placeholder := arg(value)
				*conds = append(*conds, "(payload->>'activity_type'="+placeholder+" OR payload->>'top_activity_type'="+placeholder+")")
			} else {
				*conds = append(*conds, "payload->>'"+field+"'="+arg(value))
			}
		}
	}
	if value := strings.TrimSpace(q.Get("duration_months")); value != "" {
		if number, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64); err == nil && number >= 0 {
			*conds = append(*conds, "COALESCE((payload->>'duration_months')::numeric,0)="+arg(number))
		}
	}
}

// Categories возвращает справочник категорий вместе с полями формы и
// формулой (через calculators.Fields) — фронтенд строит форму динамически.
func (h *EntryHandlers) Categories(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	rows, err := h.DB.QueryContext(r.Context(), `SELECT code, name, obligation, audience_scope FROM activity_categories ORDER BY code`)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	type categoryOut struct {
		models.ActivityCategory
		Fields []calculators.FieldSpec `json:"fields"`
	}
	out := make([]categoryOut, 0)
	for rows.Next() {
		var c models.ActivityCategory
		var scope pq.StringArray
		if err := rows.Scan(&c.Code, &c.Name, &c.Obligation, &scope); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		c.AudienceScope = []string(scope)
		calc, err := calculators.Get(c.Code)
		fields := []calculators.FieldSpec{}
		if err == nil {
			fields = calc.Fields()
		}
		out = append(out, categoryOut{ActivityCategory: c, Fields: fields})
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

type createEntryRequest struct {
	CategoryCode    string                 `json:"category_code"`
	PartnerID       *string                `json:"partner_id"`
	AgreementID     string                 `json:"agreement_id"`
	PeriodType      string                 `json:"period_type"`
	ReportYear      int                    `json:"report_year"`
	Audience        string                 `json:"audience"`
	Payload         map[string]interface{} `json:"payload"`
	CostMethod      string                 `json:"cost_method"`
	ActualAmountRub *money.Amount          `json:"actual_amount_rub,omitempty"`
}

func resolveEntryAmount(method string, actual *money.Amount, formula money.Amount) (string, money.Amount, error) {
	if method == "" {
		method = "average"
	}
	if method == "average" {
		if actual != nil {
			return "", 0, fmt.Errorf("фактическая сумма указывается только для метода «Фактические затраты»")
		}
		return method, formula, nil
	}
	if method != "actual" || actual == nil || *actual <= 0 {
		return "", 0, fmt.Errorf("для фактических затрат укажите положительную фактическую сумму")
	}
	if err := calculators.ValidateAmount(actual.Rubles()); err != nil {
		return "", 0, err
	}
	return method, *actual, nil
}

func (h *EntryHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canCreateAnyEntry(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может создавать мероприятия")
		return
	}
	var req createEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if !canCreateEntryCategory(u, req.CategoryCode) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может создавать выбранный вид мероприятия")
		return
	}
	if !canEditEntryCategory(u, req.CategoryCode) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять выбранный вид мероприятия")
		return
	}
	if req.PeriodType != string(models.PeriodPlan) && req.PeriodType != string(models.PeriodFact) {
		middleware.WriteError(w, http.StatusBadRequest, "period_type должен быть plan или fact")
		return
	}
	if req.ReportYear < 2000 || req.ReportYear > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "report_year должен быть в диапазоне 2000–2100")
		return
	}

	calc, err := calculators.Get(req.CategoryCode)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	partnerID, err := partnerIDFromPayload(req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PartnerID != nil && strings.TrimSpace(*req.PartnerID) != partnerID {
		middleware.WriteError(w, http.StatusBadRequest, "partner_id не совпадает с выбранной образовательной организацией")
		return
	}
	if err := h.validateEntryContext(r, req.CategoryCode, req.Audience, partnerID); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	if err := h.validateAgreementContext(r, req.AgreementID, partnerID, req.CategoryCode, req.ReportYear); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	staffMemberID, err := h.resolveTeachingStaff(r, req.CategoryCode, companyID, req.Payload, false)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateMentor(r, req.CategoryCode, partnerID, "", req.Payload); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, 400, "ошибка валидации: "+err.Error())
		return
	}
	formulaAmount, tariffVersionID, err := calculateEntryAmount(r.Context(), h.DB, req.CategoryCode, models.Audience(req.Audience), req.Payload, req.ReportYear)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(formulaAmount.Rubles()); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	costMethod, amount, err := resolveEntryAmount(req.CostMethod, req.ActualAmountRub, formulaAmount)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	req.CostMethod = costMethod
	if req.PeriodType != string(models.PeriodFact) && req.CostMethod == "actual" {
		middleware.WriteError(w, http.StatusBadRequest, "фактические затраты указываются только в отчёте «Факт»")
		return
	}
	payloadJSON, _ := json.Marshal(req.Payload)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	var id string
	mentor := mentorColumns(req.CategoryCode, req.Payload)
	ministry := ministryCardColumns(req.CategoryCode, req.Payload)
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO entries (category_code, partner_id, agreement_id, period_type, report_year, audience, payload, amount_rub,formula_amount_rub,actual_amount_rub,cost_method,it_company_id,staff_member_id,tariff_version_id,created_by,
		 mentor_id,mentor_assignment_start,mentor_assignment_end,mentor_order_number,mentor_order_date,assigned_student_name,
		 ministry_instruction_type,ministry_instruction_authority,ministry_instruction_reference,ministry_decision_number,ministry_decision_date,
		 ministry_implementation_start,ministry_implementation_deadline,ministry_implementation_conditions,ministry_activity_description,ministry_card_backfill_status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,'')::uuid,$14,$15,NULLIF($16,'')::uuid,NULLIF($17,'')::date,NULLIF($18,'')::date,NULLIF($19,''),NULLIF($20,'')::date,NULLIF(lower($21),''),
		 NULLIF($22,''),NULLIF($23,''),NULLIF($24,''),NULLIF($25,''),NULLIF($26,'')::date,NULLIF($27,'')::date,NULLIF($28,'')::date,NULLIF($29,''),NULLIF($30,''),NULLIF($31,'')) RETURNING id`,
		req.CategoryCode, partnerID, req.AgreementID, req.PeriodType, req.ReportYear, req.Audience, payloadJSON, amount, formulaAmount, req.ActualAmountRub, req.CostMethod, companyID, staffMemberID, tariffVersionID, u.ID,
		mentor.ID, mentor.Start, mentor.End, mentor.OrderNumber, mentor.OrderDate, mentor.Student,
		ministry.InstructionType, ministry.Authority, ministry.InstructionReference, ministry.DecisionNumber, ministry.DecisionDate,
		ministry.Start, ministry.Deadline, ministry.Conditions, ministry.Description, ministry.Status,
	).Scan(&id)
	if err != nil {
		// TCH-01: атомарный ключ педнагрузки. Повтор — это не сбой сервера, а
		// попытка внести уже учтённую нагрузку, и сказать об этом надо прямо.
		if isDuplicateTeachingLoad(err) {
			middleware.WriteError(w, http.StatusConflict,
				"такая нагрузка уже внесена: сотрудник, учебное заведение, дисциплина, семестр и период совпадают")
			return
		}
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}

	if err := logAudit(r.Context(), tx, "entry", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
		return
	}
	if req.CategoryCode == "minc_decision" {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO ministry_cost_revisions(entry_id,revision_no,confirmed_amount_rub,calculation_basis,correction_reason,changed_by)
			VALUES($1,1,$2,$3,'Начальная редакция подтверждённой стоимости',$4)`, id, amount, strings.TrimSpace(fmt.Sprint(req.Payload["calculation_basis"])), u.ID); err != nil {
			middleware.WriteError(w, 500, "ошибка сохранения истории подтверждённой стоимости")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка завершения транзакции")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "amount_rub": amount})
}

func (h *EntryHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	conds := []string{"1=1"}
	args := []interface{}{}
	arg := func(v interface{}) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if scope := partnerScope(u, ""); scope != "" {
		conds = append(conds, "partner_id::text = "+arg(scope))
	}
	if v := q.Get("report_year"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil || y < 2000 || y > 2100 {
			middleware.WriteError(w, 400, "некорректный год")
			return
		}
	}
	if v := q.Get("category_code"); v != "" {
		conds = append(conds, "category_code = "+arg(v))
	}
	if v := q.Get("period_type"); v != "" {
		conds = append(conds, "period_type = "+arg(v))
	}
	if v := q.Get("report_year"); v != "" {
		conds = append(conds, "report_year = "+arg(v))
	}
	if v := q.Get("partner_id"); v != "" {
		conds = append(conds, "partner_id::text = "+arg(v))
	}
	if v := q.Get("agreement_id"); v != "" {
		conds = append(conds, "agreement_id::text = "+arg(v))
	}
	if v := q.Get("audience"); v != "" {
		conds = append(conds, "audience = "+arg(v))
	}
	if company := itCompanyScope(u); company != "" {
		conds = append(conds, "it_company_id::text = "+arg(company))
	}
	appendEntryDetailFilters(q, &conds, arg)

	offset := 0
	if raw := q.Get("offset"); raw != "" {
		var e error
		offset, e = strconv.Atoi(raw)
		if e != nil || offset < 0 || offset > 1000000 {
			middleware.WriteError(w, 400, "некорректная страница")
			return
		}
	}
	// SQL structure comes only from fixed fragments; values remain positional parameters.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT id,COALESCE(it_company_id::text,''), category_code, partner_id, COALESCE(agreement_id::text,''), period_type, report_year, audience, payload, amount_rub,formula_amount_rub,actual_amount_rub,cost_method,COALESCE(ministry_card_backfill_status,''),
		created_by, updated_by, created_at, updated_at,
		ARRAY(SELECT DISTINCT a.document_type||':'||a.review_status FROM attachments a WHERE a.entry_id=entries.id AND a.retention_expires_at>now())
		FROM entries WHERE ` + joinAnd(conds) + ` ORDER BY updated_at DESC,id LIMIT 201 OFFSET ` + arg(offset)
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	out := make([]models.Entry, 0)
	for rows.Next() {
		var e models.Entry
		var partnerID, updatedBy sql.NullString
		var actualAmount sql.NullString
		var payloadRaw []byte
		var documentTypes pq.StringArray
		if err := rows.Scan(&e.ID, &e.ITCompanyID, &e.CategoryCode, &partnerID, &e.AgreementID, &e.PeriodType, &e.ReportYear, &e.Audience,
			&payloadRaw, &e.AmountRub, &e.FormulaAmountRub, &actualAmount, &e.CostMethod, &e.MinistryCardBackfillStatus, &e.CreatedBy, &updatedBy, &e.CreatedAt, &e.UpdatedAt, &documentTypes); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		if partnerID.Valid {
			p := partnerID.String
			e.PartnerID = &p
		}
		if updatedBy.Valid {
			p := updatedBy.String
			e.UpdatedBy = &p
		}
		if actualAmount.Valid {
			parsed, parseErr := money.Parse(actualAmount.String)
			if parseErr != nil {
				middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения фактической суммы")
				return
			}
			e.ActualAmountRub = &parsed
		}
		json.Unmarshal(payloadRaw, &e.Payload)
		e.Compliance = compliance.Evaluate(e.CategoryCode, string(e.PeriodType), e.Payload, []string(documentTypes))
		out = append(out, e)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения записей")
		return
	}
	if len(out) > 200 {
		out = out[:200]
		w.Header().Set("X-Next-Offset", strconv.Itoa(offset+200))
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

func (h *EntryHandlers) Summary(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	conds := []string{"1=1"}
	args := []interface{}{}
	arg := func(value interface{}) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if scope := partnerScope(u, ""); scope != "" {
		conds = append(conds, "partner_id::text="+arg(scope))
	}
	for _, field := range []string{"category_code", "period_type", "audience"} {
		if value := strings.TrimSpace(q.Get(field)); value != "" {
			conds = append(conds, field+"="+arg(value))
		}
	}
	if value := strings.TrimSpace(q.Get("report_year")); value != "" {
		year, err := strconv.Atoi(value)
		if err != nil || year < 2000 || year > 2100 {
			middleware.WriteError(w, 400, "некорректный год")
			return
		}
		conds = append(conds, "report_year="+arg(year))
	}
	for _, field := range []string{"partner_id", "agreement_id"} {
		if value := strings.TrimSpace(q.Get(field)); value != "" {
			conds = append(conds, field+"::text="+arg(value))
		}
	}
	if company := itCompanyScope(u); company != "" {
		conds = append(conds, "it_company_id::text="+arg(company))
	}
	appendEntryDetailFilters(q, &conds, arg)
	where := joinAnd(conds)
	var count, partners, teachers, mentors, students, courses, programs int
	var total money.Amount
	var academicHours float64
	// where contains fixed SQL fragments and $N placeholders; request values are in args.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	err := h.DB.QueryRowContext(r.Context(), `SELECT count(*),COALESCE(sum(amount_rub),0),count(DISTINCT partner_id),
		count(DISTINCT NULLIF(payload->>'teacher_full_name','')),count(DISTINCT NULLIF(payload->>'mentor_id','')),
		count(DISTINCT NULLIF(payload->>'student_full_name','')),count(DISTINCT NULLIF(payload->>'course_name','')),
		count(DISTINCT NULLIF(payload->>'program_name','')),COALESCE(sum(CASE WHEN payload ? 'academic_hours' THEN (payload->>'academic_hours')::numeric ELSE 0 END),0)::float8
		FROM entries WHERE `+where, args...).Scan(&count, &total, &partners, &teachers, &mentors, &students, &courses, &programs, &academicHours)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка расчёта итогов")
		return
	}
	type matrixItem struct {
		DocumentType string       `json:"document_type"`
		ActivityType string       `json:"activity_type"`
		Count        int          `json:"count"`
		Amount       money.Amount `json:"amount_rub"`
	}
	matrix := []matrixItem{}
	// where contains fixed SQL fragments and $N placeholders; request values are in args.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	matrixRows, err := h.DB.QueryContext(r.Context(), `SELECT payload->>'doc_type',payload->>'activity_type',count(*),COALESCE(sum(amount_rub),0) FROM entries WHERE `+where+` AND category_code='ood_rpd' GROUP BY 1,2 ORDER BY 1,2`, args...)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сводки ООП/РПД")
		return
	}
	for matrixRows.Next() {
		var item matrixItem
		if matrixRows.Scan(&item.DocumentType, &item.ActivityType, &item.Count, &item.Amount) != nil {
			matrixRows.Close()
			middleware.WriteError(w, 500, "ошибка сводки ООП/РПД")
			return
		}
		matrix = append(matrix, item)
	}
	if err = matrixRows.Err(); err != nil {
		matrixRows.Close()
		middleware.WriteError(w, 500, "ошибка сводки ООП/РПД")
		return
	}
	matrixRows.Close()
	type unitItem struct {
		Unit   string       `json:"unit"`
		Count  int          `json:"count"`
		Amount money.Amount `json:"amount_rub"`
	}
	units := []unitItem{}
	// where contains fixed SQL fragments and $N placeholders; request values are in args.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	unitRows, err := h.DB.QueryContext(r.Context(), `SELECT COALESCE(NULLIF(concat_ws(' / ',NULLIF(payload->>'institute',''),NULLIF(payload->>'faculty',''),NULLIF(payload->>'department','')),''),'Не указано'),count(*),COALESCE(sum(amount_rub),0) FROM entries WHERE `+where+` AND category_code='teachers' GROUP BY 1 ORDER BY 1`, args...)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка отчётности по подразделениям")
		return
	}
	for unitRows.Next() {
		var item unitItem
		if unitRows.Scan(&item.Unit, &item.Count, &item.Amount) != nil {
			unitRows.Close()
			middleware.WriteError(w, 500, "ошибка отчётности по подразделениям")
			return
		}
		units = append(units, item)
	}
	if err = unitRows.Err(); err != nil {
		unitRows.Close()
		middleware.WriteError(w, 500, "ошибка отчётности по подразделениям")
		return
	}
	unitRows.Close()
	middleware.WriteJSON(w, 200, map[string]interface{}{
		"count": count, "amount_rub": total, "partners_count": partners,
		"teachers_count": teachers, "mentors_count": mentors, "students_count": students,
		"courses_count": courses, "programs_count": programs, "academic_hours": academicHours,
		"ood_rpd_matrix": matrix, "structural_units": units,
	})
}

type updateEntryRequest struct {
	Payload         map[string]interface{} `json:"payload"`
	Audience        string                 `json:"audience"`
	AgreementID     string                 `json:"agreement_id"`
	Comment         string                 `json:"comment"` // ОБЯЗАТЕЛЕН по ТЗ при любом редактировании плана/факта
	CostMethod      string                 `json:"cost_method"`
	ActualAmountRub *money.Amount          `json:"actual_amount_rub,omitempty"`
}

func (h *EntryHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !canEditAnyEntry(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять мероприятия")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var req updateEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.Comment = strings.TrimSpace(req.Comment)
	if req.Comment == "" {
		middleware.WriteError(w, http.StatusBadRequest, "комментарий обязателен при редактировании отчёта")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	var categoryCode, audience, oldAgreementID, periodType, oldCostMethod string
	var reportYear int
	var oldPartnerID sql.NullString
	var oldPayloadRaw []byte
	var oldAmount, oldFormulaAmount money.Amount
	var oldActualAmount *money.Amount
	err = tx.QueryRowContext(r.Context(), `SELECT category_code,partner_id,COALESCE(agreement_id::text,''),report_year,period_type,audience,payload,amount_rub,formula_amount_rub,actual_amount_rub,cost_method FROM entries WHERE id=$1 FOR UPDATE`, entryID).
		Scan(&categoryCode, &oldPartnerID, &oldAgreementID, &reportYear, &periodType, &audience, &oldPayloadRaw, &oldAmount, &oldFormulaAmount, &oldActualAmount, &oldCostMethod)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "запись не найдена")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	if !canEditEntryCategory(u, categoryCode) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять выбранный вид мероприятия")
		return
	}
	oldAudience := audience
	if u.Role == models.RoleFinancialSpecialist {
		if categoryCode != "teachers" || !financialUpdateAllowed(oldPayloadRaw, req, oldAudience, oldAgreementID, oldCostMethod, oldActualAmount) {
			middleware.WriteError(w, http.StatusForbidden, "финансовая роль может изменять только поля компенсационной выплаты")
			return
		}
		req.Audience = oldAudience
		req.AgreementID = oldAgreementID
		req.CostMethod = oldCostMethod
		req.ActualAmountRub = oldActualAmount
	}
	if req.Audience != "" {
		audience = req.Audience
	}

	calc, err := calculators.Get(categoryCode)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	partnerID, err := partnerIDFromPayload(req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateEntryContext(r, categoryCode, audience, partnerID); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	if oldPartnerID.String != partnerID {
		middleware.WriteError(w, 400, "перенос записи к другому партнёру не допускается")
		return
	}
	if req.AgreementID == "" {
		req.AgreementID = oldAgreementID
	}
	if err := h.validateAgreementContext(r, req.AgreementID, partnerID, categoryCode, reportYear); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	staffMemberID, err := h.resolveTeachingStaff(r, categoryCode, companyID, req.Payload, false)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateMentor(r, categoryCode, partnerID, entryID, req.Payload); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, 400, "ошибка валидации: "+err.Error())
		return
	}
	formulaAmount, tariffVersionID, err := calculateEntryAmount(r.Context(), h.DB, categoryCode, models.Audience(audience), req.Payload, reportYear)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(formulaAmount.Rubles()); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	costMethod, newAmount, err := resolveEntryAmount(req.CostMethod, req.ActualAmountRub, formulaAmount)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	req.CostMethod = costMethod
	if periodType != string(models.PeriodFact) && req.CostMethod == "actual" {
		middleware.WriteError(w, http.StatusBadRequest, "фактические затраты указываются только в отчёте «Факт»")
		return
	}
	newPayloadJSON, _ := json.Marshal(req.Payload)

	mentor := mentorColumns(categoryCode, req.Payload)
	ministry := ministryCardColumns(categoryCode, req.Payload)
	_, err = tx.ExecContext(r.Context(),
		`UPDATE entries SET payload=$1,audience=$2,partner_id=$3,agreement_id=$4,amount_rub=$5,formula_amount_rub=$6,actual_amount_rub=$7,cost_method=$8,staff_member_id=COALESCE(NULLIF($9,'')::uuid,staff_member_id),tariff_version_id=$10,updated_by=$11,updated_at=now(),
		 mentor_id=NULLIF($13,'')::uuid,mentor_assignment_start=NULLIF($14,'')::date,mentor_assignment_end=NULLIF($15,'')::date,mentor_order_number=NULLIF($16,''),mentor_order_date=NULLIF($17,'')::date,assigned_student_name=NULLIF(lower($18),''),
		 ministry_instruction_type=NULLIF($19,''),ministry_instruction_authority=NULLIF($20,''),ministry_instruction_reference=NULLIF($21,''),ministry_decision_number=NULLIF($22,''),
		 ministry_decision_date=NULLIF($23,'')::date,ministry_implementation_start=NULLIF($24,'')::date,ministry_implementation_deadline=NULLIF($25,'')::date,
		 ministry_implementation_conditions=NULLIF($26,''),ministry_activity_description=NULLIF($27,''),ministry_card_backfill_status=NULLIF($28,'')
		 WHERE id=$12`,
		newPayloadJSON, audience, partnerID, req.AgreementID, newAmount, formulaAmount, req.ActualAmountRub, req.CostMethod, staffMemberID, tariffVersionID, u.ID, entryID,
		mentor.ID, mentor.Start, mentor.End, mentor.OrderNumber, mentor.OrderDate, mentor.Student,
		ministry.InstructionType, ministry.Authority, ministry.InstructionReference, ministry.DecisionNumber, ministry.DecisionDate,
		ministry.Start, ministry.Deadline, ministry.Conditions, ministry.Description, ministry.Status,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	if categoryCode == "minc_decision" && newAmount != oldAmount {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO ministry_cost_revisions(entry_id,revision_no,previous_amount_rub,confirmed_amount_rub,calculation_basis,correction_reason,changed_by)
			SELECT $1,COALESCE(max(revision_no),0)+1,$2,$3,$4,$5,$6 FROM ministry_cost_revisions WHERE entry_id=$1`,
			entryID, oldAmount, newAmount, strings.TrimSpace(fmt.Sprint(req.Payload["calculation_basis"])), req.Comment, u.ID); err != nil {
			middleware.WriteError(w, 500, "ошибка сохранения истории подтверждённой стоимости")
			return
		}
	}

	_, err = tx.ExecContext(r.Context(),
		`INSERT INTO entry_comments (entry_id, user_id, comment_text) VALUES ($1, $2, $3)`,
		entryID, u.ID, req.Comment,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения комментария")
		return
	}

	var oldPayload map[string]interface{}
	json.Unmarshal(oldPayloadRaw, &oldPayload)
	oldPartner := ""
	if oldPartnerID.Valid {
		oldPartner = oldPartnerID.String
	}
	if err := logAudit(r.Context(), tx, "entry", entryID, "update", u.ID, req.Comment,
		map[string]interface{}{"partner_id": oldPartner, "agreement_id": oldAgreementID, "audience": oldAudience, "payload": oldPayload, "amount_rub": oldAmount, "formula_amount_rub": oldFormulaAmount, "actual_amount_rub": oldActualAmount, "cost_method": oldCostMethod},
		map[string]interface{}{"partner_id": partnerID, "agreement_id": req.AgreementID, "audience": audience, "payload": req.Payload, "amount_rub": newAmount, "formula_amount_rub": formulaAmount, "actual_amount_rub": req.ActualAmountRub, "cost_method": req.CostMethod},
	); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка завершения транзакции")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"amount_rub": newAmount})
}

type mentorEntryColumns struct {
	ID, Start, End, OrderNumber, OrderDate, Student string
}

func mentorColumns(category string, payload map[string]interface{}) mentorEntryColumns {
	if category != "internship" && category != "employment_practice" {
		return mentorEntryColumns{}
	}
	value := func(key string) string { return strings.TrimSpace(fmt.Sprint(payload[key])) }
	return mentorEntryColumns{
		ID: value("mentor_id"), Start: value("mentor_assignment_start"), End: value("mentor_assignment_end"),
		OrderNumber: value("mentor_order_number"), OrderDate: value("mentor_order_date"), Student: strings.Join(strings.Fields(value("student_full_name")), " "),
	}
}

type ministryEntryColumns struct {
	InstructionType, Authority, InstructionReference string
	DecisionNumber, DecisionDate, Start, Deadline    string
	Conditions, Description, Status                  string
}

func ministryCardColumns(category string, payload map[string]interface{}) ministryEntryColumns {
	if category != "minc_decision" {
		return ministryEntryColumns{}
	}
	value := func(key string) string { return strings.TrimSpace(fmt.Sprint(payload[key])) }
	return ministryEntryColumns{
		InstructionType: value("instruction_type"), Authority: value("instruction_authority"),
		InstructionReference: value("instruction_reference"), DecisionNumber: value("decision_number"),
		DecisionDate: value("decision_date"), Start: value("implementation_start"),
		Deadline: value("implementation_deadline"), Conditions: value("implementation_conditions"),
		Description: value("activity_description"), Status: "complete",
	}
}

func financialUpdateAllowed(oldPayloadRaw []byte, req updateEntryRequest, oldAudience, oldAgreementID, oldCostMethod string, oldActualAmount *money.Amount) bool {
	if req.Audience != "" && req.Audience != oldAudience || req.AgreementID != "" && req.AgreementID != oldAgreementID || req.CostMethod != "" && req.CostMethod != oldCostMethod {
		return false
	}
	if req.ActualAmountRub != nil && (oldActualAmount == nil || *req.ActualAmountRub != *oldActualAmount) {
		return false
	}
	oldPayload := map[string]interface{}{}
	if json.Unmarshal(oldPayloadRaw, &oldPayload) != nil {
		return false
	}
	allowed := map[string]bool{"compensation_quarter": true, "planned_compensation_rub": true, "payment_status": true, "payment_date": true, "payment_order_reference": true}
	for key := range allowed {
		delete(oldPayload, key)
	}
	newCore := map[string]interface{}{}
	for key, value := range req.Payload {
		if !allowed[key] {
			newCore[key] = value
		}
	}
	oldJSON, _ := json.Marshal(oldPayload)
	newJSON, _ := json.Marshal(newCore)
	if string(oldJSON) != string(newJSON) {
		return false
	}
	if fmt.Sprint(req.Payload["payment_status"]) == "paid" {
		paymentDate, dateOK := req.Payload["payment_date"].(string)
		paymentReference, referenceOK := req.Payload["payment_order_reference"].(string)
		return dateOK && referenceOK && strings.TrimSpace(paymentDate) != "" && strings.TrimSpace(paymentReference) != ""
	}
	return true
}

func (h *EntryHandlers) Comments(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, entry_id, user_id, comment_text, created_at
		FROM entry_comments WHERE entry_id = $1 ORDER BY created_at DESC,id`+page, entryID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	out := make([]models.EntryComment, 0)
	for rows.Next() {
		var c models.EntryComment
		if err := rows.Scan(&c.ID, &c.EntryID, &c.UserID, &c.CommentText, &c.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		out = append(out, c)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения комментариев")
		return
	}
	writePage(w, r, out)
}

// MinistryCostHistory exposes the append-only correction trail for a Type 5
// activity. The same tenant guard as the entry itself applies.
func (h *EntryHandlers) MinistryCostHistory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var category string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT category_code FROM entries WHERE id=$1`, entryID).Scan(&category); err != nil {
		middleware.WriteError(w, 404, "запись не найдена")
		return
	}
	if category != "minc_decision" {
		middleware.WriteError(w, 400, "история подтверждённой стоимости доступна только для мероприятий по Решению Минцифры")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT revision_no,previous_amount_rub,confirmed_amount_rub,calculation_basis,correction_reason,changed_by,changed_at
		FROM ministry_cost_revisions WHERE entry_id=$1 ORDER BY revision_no DESC`, entryID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения истории стоимости")
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var revision int
		var previous *money.Amount
		var confirmed money.Amount
		var basis, reason, actor string
		var changed time.Time
		if err := rows.Scan(&revision, &previous, &confirmed, &basis, &reason, &actor, &changed); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения истории стоимости")
			return
		}
		items = append(items, map[string]interface{}{"revision_no": revision, "previous_amount_rub": previous, "confirmed_amount_rub": confirmed, "calculation_basis": basis, "correction_reason": reason, "changed_by": actor, "changed_at": changed})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения истории стоимости")
		return
	}
	middleware.WriteJSON(w, 200, items)
}

func joinAnd(conds []string) string {
	out := conds[0]
	for _, c := range conds[1:] {
		out += " AND " + c
	}
	return out
}

func partnerIDFromPayload(payload map[string]interface{}) (string, error) {
	v, ok := payload["org_name"]
	if !ok {
		return "", fmt.Errorf("поле %q обязательно", "org_name")
	}
	id, ok := v.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("поле %q должно содержать выбранную организацию", "org_name")
	}
	return strings.TrimSpace(id), nil
}

func (h *EntryHandlers) validateEntryContext(r *http.Request, categoryCode, audience, partnerID string) error {
	var audienceAllowed bool
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT $2::text = ANY(audience_scope) FROM activity_categories WHERE code = $1`,
		categoryCode, audience,
	).Scan(&audienceAllowed)
	if err == sql.ErrNoRows {
		return fmt.Errorf("неизвестная категория активности: %s", categoryCode)
	}
	if err != nil {
		return fmt.Errorf("не удалось проверить категорию")
	}
	if !audienceAllowed {
		return fmt.Errorf("аудитория %q недопустима для выбранной категории", audience)
	}

	var partnerKind string
	err = h.DB.QueryRowContext(r.Context(), `SELECT partner_kind FROM partners WHERE id::text = $1`, partnerID).Scan(&partnerKind)
	if err == sql.ErrNoRows {
		return fmt.Errorf("выбранная образовательная организация не найдена")
	}
	if err != nil {
		return fmt.Errorf("не удалось проверить образовательную организацию")
	}
	if partnerKind != audience {
		return fmt.Errorf("вид выбранной организации %q не соответствует аудитории %q", partnerKind, audience)
	}
	return nil
}

func (h *EntryHandlers) validateAgreementContext(r *http.Request, agreementID, partnerID, categoryCode string, reportYear int) error {
	agreementID = strings.TrimSpace(agreementID)
	if agreementID == "" {
		return fmt.Errorf("выберите соглашение, к которому относится активность")
	}
	var status, agreementKind, regionalAuthorityID, regionalAuthorityStatus, partnerKind string
	var validFrom, validUntil time.Time
	err := h.DB.QueryRowContext(r.Context(), `SELECT a.status,a.valid_from,a.valid_until,a.agreement_kind,
		COALESCE(a.regional_authority_id::text,''),COALESCE(ra.status,''),p.partner_kind
		FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id
		JOIN partners p ON p.id=ap.partner_id
		LEFT JOIN regional_authorities ra ON ra.id=a.regional_authority_id
		WHERE a.id::text=$1 AND ap.partner_id::text=$2`, agreementID, partnerID).
		Scan(&status, &validFrom, &validUntil, &agreementKind, &regionalAuthorityID, &regionalAuthorityStatus, &partnerKind)
	if err == sql.ErrNoRows {
		return fmt.Errorf("соглашение не найдено или не относится к выбранной организации")
	}
	if err != nil {
		return fmt.Errorf("не удалось проверить соглашение")
	}
	if status != "active" {
		return fmt.Errorf("для план/факта требуется действующее соглашение; текущий статус: %s", status)
	}
	if partnerKind == "school" && (agreementKind != "roiv" || regionalAuthorityID == "" || regionalAuthorityStatus != "active") {
		return fmt.Errorf("школьная активность должна относиться к соглашению с выбранным РОИВ")
	}
	yearStart := time.Date(reportYear, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd := time.Date(reportYear, 12, 31, 0, 0, 0, 0, time.UTC)
	if validFrom.After(yearEnd) || validUntil.Before(yearStart) {
		return fmt.Errorf("соглашение не действует в %d году", reportYear)
	}
	var included bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM agreement_activity_requirements WHERE agreement_id::text=$1 AND category_code=$2)`, agreementID, categoryCode).Scan(&included); err != nil {
		return fmt.Errorf("не удалось проверить перечень мероприятий соглашения")
	}
	if !included {
		return fmt.Errorf("выбранный вид мероприятия не включён в перечень соглашения")
	}
	return nil
}

// isDuplicateTeachingLoad распознаёт нарушение атомарного ключа педнагрузки.
func isDuplicateTeachingLoad(err error) bool {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.Constraint == "teaching_load_atomic_key_idx"
}
