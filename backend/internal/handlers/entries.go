package handlers

import (
	"cybercalc/internal/money"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"cybercalc/internal/calculators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type EntryHandlers struct {
	DB *sql.DB
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
	CategoryCode string                 `json:"category_code"`
	PartnerID    *string                `json:"partner_id"`
	PeriodType   string                 `json:"period_type"`
	ReportYear   int                    `json:"report_year"`
	Audience     string                 `json:"audience"`
	Payload      map[string]interface{} `json:"payload"`
}

func (h *EntryHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req createEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
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
	if !requirePartner(w, u, partnerID) {
		return
	}
	if err := h.validateMentor(r, req.CategoryCode, partnerID, req.Payload); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, 400, "ошибка валидации: "+err.Error())
		return
	}
	amount, err := calculators.CalculateAmount(req.CategoryCode, models.Audience(req.Audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(amount.Rubles()); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
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
	err = tx.QueryRowContext(r.Context(),
		`INSERT INTO entries (category_code, partner_id, period_type, report_year, audience, payload, amount_rub, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.CategoryCode, partnerID, req.PeriodType, req.ReportYear, req.Audience, payloadJSON, amount, u.ID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}

	if err := logAudit(tx, "entry", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
		return
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
	if v := q.Get("audience"); v != "" {
		conds = append(conds, "audience = "+arg(v))
	}

	offset := 0
	if raw := q.Get("offset"); raw != "" {
		var e error
		offset, e = strconv.Atoi(raw)
		if e != nil || offset < 0 || offset > 1000000 {
			middleware.WriteError(w, 400, "некорректная страница")
			return
		}
	}
	query := `SELECT id, category_code, partner_id, period_type, report_year, audience, payload, amount_rub,
		created_by, updated_by, created_at, updated_at FROM entries WHERE ` + joinAnd(conds) + ` ORDER BY updated_at DESC,id LIMIT 201 OFFSET ` + arg(offset)
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
		var payloadRaw []byte
		if err := rows.Scan(&e.ID, &e.CategoryCode, &partnerID, &e.PeriodType, &e.ReportYear, &e.Audience,
			&payloadRaw, &e.AmountRub, &e.CreatedBy, &updatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
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
		json.Unmarshal(payloadRaw, &e.Payload)
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

type updateEntryRequest struct {
	Payload  map[string]interface{} `json:"payload"`
	Audience string                 `json:"audience"`
	Comment  string                 `json:"comment"` // ОБЯЗАТЕЛЕН по ТЗ при любом редактировании плана/факта
}

func (h *EntryHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !requireEntry(w, h.DB, u, entryID) {
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

	var categoryCode, audience string
	var oldPartnerID sql.NullString
	var oldPayloadRaw []byte
	var oldAmount money.Amount
	err = tx.QueryRowContext(r.Context(), `SELECT category_code, partner_id, audience, payload, amount_rub FROM entries WHERE id = $1 FOR UPDATE`, entryID).
		Scan(&categoryCode, &oldPartnerID, &audience, &oldPayloadRaw, &oldAmount)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "запись не найдена")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	oldAudience := audience
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
	if !requirePartner(w, u, partnerID) {
		return
	}
	if oldPartnerID.String != partnerID {
		middleware.WriteError(w, 400, "перенос записи к другому партнёру не допускается")
		return
	}
	if err := h.validateMentor(r, categoryCode, partnerID, req.Payload); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, 400, "ошибка валидации: "+err.Error())
		return
	}
	newAmount, err := calculators.CalculateAmount(categoryCode, models.Audience(audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(newAmount.Rubles()); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	newPayloadJSON, _ := json.Marshal(req.Payload)

	_, err = tx.ExecContext(r.Context(),
		`UPDATE entries SET payload = $1, audience = $2, partner_id = $3, amount_rub = $4, updated_by = $5, updated_at = now()
		 WHERE id = $6`,
		newPayloadJSON, audience, partnerID, newAmount, u.ID, entryID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
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
	if err := logAudit(tx, "entry", entryID, "update", u.ID, req.Comment,
		map[string]interface{}{"partner_id": oldPartner, "audience": oldAudience, "payload": oldPayload, "amount_rub": oldAmount},
		map[string]interface{}{"partner_id": partnerID, "audience": audience, "payload": req.Payload, "amount_rub": newAmount},
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

func (h *EntryHandlers) Comments(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	if !requireEntry(w, h.DB, u, entryID) {
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
