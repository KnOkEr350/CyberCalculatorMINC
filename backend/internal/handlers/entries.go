package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"cybercalc/internal/calculators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type EntryHandlers struct {
	DB        *sql.DB
	UploadDir string
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
	var document multipart.File
	var documentHeader *multipart.FileHeader
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+(2<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "не удалось разобрать запись и подтверждающий документ")
			return
		}
		if err := json.Unmarshal([]byte(r.FormValue("entry")), &req); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "поле entry должно содержать корректную запись в формате JSON")
			return
		}
		var err error
		document, documentHeader, err = r.FormFile("file")
		if err != nil && !errors.Is(err, http.ErrMissingFile) {
			middleware.WriteError(w, http.StatusBadRequest, "не удалось прочитать подтверждающий документ")
			return
		}
		if document != nil {
			defer document.Close()
		}
	} else if err := decodeJSON(r, &req); err != nil {
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
	if req.PeriodType == string(models.PeriodFact) && (document == nil || documentHeader == nil) {
		middleware.WriteError(w, http.StatusBadRequest, "для фактической записи обязателен подтверждающий документ")
		return
	}

	calc, err := calculators.Get(req.CategoryCode)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка валидации: "+err.Error())
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
	if u.Role != models.RoleAdmin && u.PartnerID != nil && partnerID != *u.PartnerID {
		middleware.WriteError(w, http.StatusForbidden, "можно добавлять данные только для назначенного партнёра")
		return
	}
	if err := h.validateEntryContext(req.CategoryCode, req.Audience, partnerID); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	amount, err := calc.Calculate(models.Audience(req.Audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(amount); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}

	payloadJSON, _ := json.Marshal(req.Payload)
	tx, err := h.DB.Begin()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка начала транзакции")
		return
	}
	defer tx.Rollback()
	var storedDocument storedAttachment
	committed := false
	defer func() {
		if !committed && storedDocument.Path != "" {
			_ = os.Remove(storedDocument.Path)
		}
	}()

	var id string
	err = tx.QueryRow(
		`INSERT INTO entries (category_code, partner_id, period_type, report_year, audience, payload, amount_rub, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.CategoryCode, partnerID, req.PeriodType, req.ReportYear, req.Audience, payloadJSON, amount, u.ID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения: "+err.Error())
		return
	}
	if req.PeriodType == string(models.PeriodFact) {
		storedDocument, err = storeAttachment(tx, h.UploadDir, id, u.ID, document, documentHeader)
		if err != nil {
			if errors.Is(err, errAttachmentTooLarge) {
				middleware.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
			} else {
				middleware.WriteError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		if err := logAudit(tx, "attachment", storedDocument.ID, "upload", u.ID,
			fmt.Sprintf("файл %s (%d байт)", storedDocument.FileName, storedDocument.Size), nil,
			map[string]interface{}{"entry_id": id, "file_name": storedDocument.FileName}); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
			return
		}
	}

	if err := logAudit(tx, "entry", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка завершения транзакции")
		return
	}
	committed = true
	response := map[string]interface{}{"id": id, "amount_rub": amount}
	if storedDocument.ID != "" {
		response["attachment_id"] = storedDocument.ID
	}
	middleware.WriteJSON(w, http.StatusCreated, response)
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
	conds, args = appendEntryScope(conds, args, u, "")

	query := `SELECT id, category_code, partner_id, period_type, report_year, audience, payload, amount_rub,
		created_by, updated_by, created_at, updated_at FROM entries WHERE ` + joinAnd(conds) + ` ORDER BY updated_at DESC`
	rows, err := h.DB.Query(query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса: "+err.Error())
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
	middleware.WriteJSON(w, http.StatusOK, out)
}

type updateEntryRequest struct {
	Payload  map[string]interface{} `json:"payload"`
	Audience string                 `json:"audience"`
	Comment  string                 `json:"comment"` // ОБЯЗАТЕЛЕН по ТЗ при любом редактировании плана/факта
}

func (h *EntryHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
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

	tx, err := h.DB.Begin()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	conditions, scopeArgs := appendEntryScope([]string{"id = $1"}, []interface{}{entryID}, u, "")
	var categoryCode, audience, periodType string
	var oldPartnerID sql.NullString
	var oldPayloadRaw []byte
	var oldAmount float64
	err = tx.QueryRow(`SELECT category_code, partner_id, audience, period_type, payload, amount_rub FROM entries WHERE `+
		strings.Join(conditions, " AND ")+` FOR UPDATE`, scopeArgs...).
		Scan(&categoryCode, &oldPartnerID, &audience, &periodType, &oldPayloadRaw, &oldAmount)
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
	if err := calculators.ValidatePayload(calc, req.Payload); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка валидации: "+err.Error())
		return
	}
	partnerID, err := partnerIDFromPayload(req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.validateEntryContext(categoryCode, audience, partnerID); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if u.Role != models.RoleAdmin && u.PartnerID != nil && partnerID != *u.PartnerID {
		middleware.WriteError(w, http.StatusForbidden, "можно изменять данные только назначенного партнёра")
		return
	}
	if periodType == string(models.PeriodFact) {
		var documents int
		if err := tx.QueryRow(`SELECT count(*) FROM attachments WHERE entry_id = $1`, entryID).Scan(&documents); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка проверки подтверждающего документа")
			return
		}
		if documents == 0 {
			middleware.WriteError(w, http.StatusBadRequest, "для фактической записи обязателен подтверждающий документ")
			return
		}
	}
	newAmount, err := calc.Calculate(models.Audience(audience), req.Payload)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	if err := calculators.ValidateAmount(newAmount); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "ошибка расчёта: "+err.Error())
		return
	}
	newPayloadJSON, _ := json.Marshal(req.Payload)

	_, err = tx.Exec(
		`UPDATE entries SET payload = $1, audience = $2, partner_id = $3, amount_rub = $4, updated_by = $5, updated_at = now()
		 WHERE id = $6`,
		newPayloadJSON, audience, partnerID, newAmount, u.ID, entryID,
	)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}

	_, err = tx.Exec(
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
	conditions, args := appendEntryScope([]string{"c.entry_id = $1"}, []interface{}{entryID}, u, "e")
	rows, err := h.DB.Query(`SELECT c.id, c.entry_id, c.user_id, c.comment_text, c.created_at
		FROM entry_comments c JOIN entries e ON e.id = c.entry_id WHERE `+strings.Join(conditions, " AND ")+` ORDER BY c.created_at DESC`, args...)
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
	middleware.WriteJSON(w, http.StatusOK, out)
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

func (h *EntryHandlers) validateEntryContext(categoryCode, audience, partnerID string) error {
	var audienceAllowed bool
	err := h.DB.QueryRow(
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
	err = h.DB.QueryRow(`SELECT partner_kind FROM partners WHERE id::text = $1`, partnerID).Scan(&partnerKind)
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
