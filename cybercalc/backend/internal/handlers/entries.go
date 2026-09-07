package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

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
	rows, err := h.DB.Query(`SELECT code, name, obligation, audience_scope FROM activity_categories ORDER BY code`)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	type categoryOut struct {
		models.ActivityCategory
		Fields []calculators.FieldSpec `json:"fields"`
	}
	var out []categoryOut
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.PeriodType != string(models.PeriodPlan) && req.PeriodType != string(models.PeriodFact) {
		middleware.WriteError(w, http.StatusBadRequest, "period_type должен быть plan или fact")
		return
	}

	calc, err := calculators.Get(req.CategoryCode)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := calc.Calculate(models.Audience(req.Audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}

	payloadJSON, _ := json.Marshal(req.Payload)
	var id string
	err = h.DB.QueryRow(
		`INSERT INTO entries (category_code, partner_id, period_type, report_year, audience, payload, amount_rub, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.CategoryCode, req.PartnerID, req.PeriodType, req.ReportYear, req.Audience, payloadJSON, amount, u.ID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения: "+err.Error())
		return
	}

	logAudit(h.DB, "entry", id, "create", u.ID, "", nil, req)
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
		conds = append(conds, "partner_id = "+arg(v))
	}
	if v := q.Get("audience"); v != "" {
		conds = append(conds, "audience = "+arg(v))
	}

	query := `SELECT id, category_code, partner_id, period_type, report_year, audience, payload, amount_rub,
		created_by, updated_by, created_at, updated_at FROM entries WHERE ` + joinAnd(conds) + ` ORDER BY updated_at DESC`
	rows, err := h.DB.Query(query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса: "+err.Error())
		return
	}
	defer rows.Close()

	var out []models.Entry
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
	middleware.WriteJSON(w, http.StatusOK, out)
}

type updateEntryRequest struct {
	Payload  map[string]interface{} `json:"payload"`
	Audience string                 `json:"audience"`
	Comment  string                 `json:"comment"` // ОБЯЗАТЕЛЕН по ТЗ при любом редактировании плана/факта
}

func (h *EntryHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	var req updateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.Comment == "" {
		middleware.WriteError(w, http.StatusBadRequest, "комментарий обязателен при редактировании отчёта")
		return
	}

	var categoryCode, audience string
	var oldPayloadRaw []byte
	var oldAmount float64
	err := h.DB.QueryRow(`SELECT category_code, audience, payload, amount_rub FROM entries WHERE id = $1`, entryID).
		Scan(&categoryCode, &audience, &oldPayloadRaw, &oldAmount)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "запись не найдена")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	if req.Audience != "" {
		audience = req.Audience
	}

	calc, err := calculators.Get(categoryCode)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newAmount, err := calc.Calculate(models.Audience(audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	newPayloadJSON, _ := json.Marshal(req.Payload)

	_, err = h.DB.Exec(
		`UPDATE entries SET payload = $1, audience = $2, amount_rub = $3, updated_by = $4, updated_at = now()
		 WHERE id = $5`,
		newPayloadJSON, audience, newAmount, u.ID, entryID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}

	_, err = h.DB.Exec(
		`INSERT INTO entry_comments (entry_id, user_id, comment_text) VALUES ($1, $2, $3)`,
		entryID, u.ID, req.Comment,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения комментария")
		return
	}

	var oldPayload map[string]interface{}
	json.Unmarshal(oldPayloadRaw, &oldPayload)
	logAudit(h.DB, "entry", entryID, "update", u.ID, req.Comment,
		map[string]interface{}{"payload": oldPayload, "amount_rub": oldAmount},
		map[string]interface{}{"payload": req.Payload, "amount_rub": newAmount},
	)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"amount_rub": newAmount})
}

func (h *EntryHandlers) Comments(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	rows, err := h.DB.Query(`SELECT id, entry_id, user_id, comment_text, created_at
		FROM entry_comments WHERE entry_id = $1 ORDER BY created_at DESC`, entryID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	var out []models.EntryComment
	for rows.Next() {
		var c models.EntryComment
		if err := rows.Scan(&c.ID, &c.EntryID, &c.UserID, &c.CommentText, &c.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		out = append(out, c)
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

func joinAnd(conds []string) string {
	out := conds[0]
	for _, c := range conds[1:] {
		out += " AND " + c
	}
	return out
}
