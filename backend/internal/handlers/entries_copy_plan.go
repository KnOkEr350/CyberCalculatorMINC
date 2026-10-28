package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

const copyPlanLimit = 1000

type copyPlanRequest struct {
	AgreementID  string `json:"agreement_id"`
	ReportYear   int    `json:"report_year"`
	CategoryCode string `json:"category_code,omitempty"`
	PartnerID    string `json:"partner_id,omitempty"`
}

type copyPlanItem struct {
	PlanEntryID  string `json:"plan_entry_id"`
	FactEntryID  string `json:"fact_entry_id,omitempty"`
	CategoryCode string `json:"category_code"`
	Reason       string `json:"reason,omitempty"`
}

// responseCapture собирает ответ, который общая часть создания записи пишет в
// http.ResponseWriter: при массовом копировании ошибка одной записи не должна
// обрывать остальные.
type responseCapture struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (c *responseCapture) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}
func (c *responseCapture) Write(b []byte) (int, error) { return c.body.Write(b) }
func (c *responseCapture) WriteHeader(code int)        { c.code = code }

func (c *responseCapture) message() string {
	var out struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(c.body.Bytes(), &out) == nil && out.Error != "" {
		return out.Error
	}
	return "не удалось создать запись"
}

// CopyPlanToFact создаёт записи факта из записей плана одного соглашения и
// года. Копируются значения мероприятия; расчёт, права и проверки те же, что
// при ручном вводе, а сумма считается по методике (фактические затраты и
// вложения не переносятся - их подтверждает сам факт). Запись плана уже
// скопирована или такая запись факта уже есть - пропускается.
func (h *EntryHandlers) CopyPlanToFact(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canCreateAnyEntry(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может создавать мероприятия")
		return
	}
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	var req copyPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.AgreementID = strings.TrimSpace(req.AgreementID)
	if req.AgreementID == "" || req.ReportYear < 2000 || req.ReportYear > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите соглашение и отчётный год")
		return
	}

	conds := []string{"period_type='plan'", "agreement_id::text=$1", "report_year=$2", "it_company_id::text=$3"}
	args := []interface{}{req.AgreementID, req.ReportYear, companyID}
	add := func(cond string, value interface{}) {
		args = append(args, value)
		conds = append(conds, strings.Replace(cond, "?", "$"+strconv.Itoa(len(args)), 1))
	}
	if v := strings.TrimSpace(req.CategoryCode); v != "" {
		add("category_code=?", v)
	}
	if v := partnerScope(u, strings.TrimSpace(req.PartnerID)); v != "" {
		add("partner_id::text=?", v)
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id::text,category_code,partner_id::text,audience,payload,
		EXISTS(SELECT 1 FROM entries f WHERE f.copied_from_entry_id=entries.id)
		FROM entries WHERE `+joinAnd(conds)+` ORDER BY created_at,id LIMIT `+strconv.Itoa(copyPlanLimit+1), args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	type planRow struct {
		id, category, partner, audience string
		payload                         []byte
		copied                          bool
	}
	var plans []planRow
	for rows.Next() {
		var p planRow
		var partner sql.NullString
		if err := rows.Scan(&p.id, &p.category, &partner, &p.audience, &p.payload, &p.copied); err != nil {
			rows.Close()
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		p.partner = partner.String
		plans = append(plans, p)
	}
	rows.Close()
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения записей плана")
		return
	}
	if len(plans) > copyPlanLimit {
		middleware.WriteError(w, http.StatusUnprocessableEntity, "в плане более 1000 записей; выберите вид мероприятия или организацию")
		return
	}
	if len(plans) == 0 {
		middleware.WriteError(w, http.StatusConflict, "в плане нет записей для копирования")
		return
	}

	created, skipped, failed := []copyPlanItem{}, []copyPlanItem{}, []copyPlanItem{}
	for _, plan := range plans {
		item := copyPlanItem{PlanEntryID: plan.id, CategoryCode: plan.category}
		if plan.copied {
			item.Reason = "уже скопировано в факт"
			skipped = append(skipped, item)
			continue
		}
		var exists bool
		if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM entries
			WHERE period_type='fact' AND agreement_id::text=$1 AND report_year=$2 AND category_code=$3
			AND partner_id::text=$4 AND payload=$5::jsonb)`, req.AgreementID, req.ReportYear, plan.category, plan.partner, string(plan.payload)).Scan(&exists); err != nil {
			item.Reason = "не удалось проверить факт"
			failed = append(failed, item)
			continue
		}
		if exists {
			item.Reason = "такая запись факта уже есть"
			skipped = append(skipped, item)
			continue
		}
		var payload map[string]interface{}
		decoder := json.NewDecoder(bytes.NewReader(plan.payload))
		decoder.UseNumber()
		if decoder.Decode(&payload) != nil {
			item.Reason = "не удалось прочитать запись плана"
			failed = append(failed, item)
			continue
		}
		partner := plan.partner
		capture := &responseCapture{}
		factID, _, ok := h.createEntry(capture, r, u, createEntryRequest{
			CategoryCode: plan.category, PartnerID: &partner, AgreementID: req.AgreementID,
			PeriodType: string(models.PeriodFact), ReportYear: req.ReportYear, Audience: plan.audience,
			Payload: payload, CostMethod: "average", copiedFrom: plan.id,
		})
		if !ok {
			item.Reason = capture.message()
			failed = append(failed, item)
			continue
		}
		item.FactEntryID = factID
		created = append(created, item)
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"created": created, "skipped": skipped, "failed": failed,
		"created_count": len(created), "skipped_count": len(skipped), "failed_count": len(failed),
	})
}
