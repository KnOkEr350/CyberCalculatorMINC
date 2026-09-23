package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"cybercalc/internal/topit"
)

// TopItemHandlers — составляющие программы ТОП-ИТ/ТОП-ИИ: неденежная
// поддержка, стипендиаты, производственные кейсы (TOP-04, TOP-06, TOP-07).
type TopItemHandlers struct{ DB *sql.DB }

// topItemRequest — строка составляющей программы; поля и правила — в topit.Input.
type topItemRequest struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Неденежная поддержка.
	SupportKind       string        `json:"support_kind,omitempty"`
	ActReference      string        `json:"act_reference,omitempty"`
	ActDate           string        `json:"act_date,omitempty"`
	BalanceValueRub   *money.Amount `json:"balance_value_rub,omitempty"`
	AppraisedValueRub *money.Amount `json:"appraised_value_rub,omitempty"`
	ConfirmedValueRub *money.Amount `json:"confirmed_value_rub,omitempty"`
	// Стипендиат.
	StudentName string        `json:"student_name,omitempty"`
	GroupName   string        `json:"group_name,omitempty"`
	Course      int           `json:"course,omitempty"`
	PeriodStart string        `json:"period_start,omitempty"`
	PeriodEnd   string        `json:"period_end,omitempty"`
	AmountRub   *money.Amount `json:"amount_rub,omitempty"`
	Criterion   string        `json:"criterion,omitempty"`
	DonorName   string        `json:"donor_name,omitempty"`
	// Производственный кейс.
	ImplementationOrg    string `json:"implementation_org,omitempty"`
	ImplementationStatus string `json:"implementation_status,omitempty"`
	ImplementedOn        string `json:"implemented_on,omitempty"`
	Description          string `json:"description,omitempty"`
	// DocumentIDs — вложения записи, подтверждающие строку.
	DocumentIDs []string `json:"document_ids,omitempty"`
}

func writeTopError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, topit.ErrNotFound):
		middleware.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, topit.ErrInvalid), errors.Is(err, topit.ErrNotTopEntry), errors.Is(err, topit.ErrForeignFiles):
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка составляющих программы")
	}
}

// loadTopEntry проверяет доступ к записи и что это запись Вида 4.
func (h *TopItemHandlers) loadTopEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string, write bool) bool {
	if !requireEntry(w, r, h.DB, u, entryID) {
		return false
	}
	var category string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT category_code FROM entries WHERE id::text=$1`, entryID).Scan(&category); err != nil {
		middleware.WriteError(w, http.StatusNotFound, "запись не найдена")
		return false
	}
	if category != "top_it" {
		writeTopError(w, topit.ErrNotTopEntry)
		return false
	}
	if write && !canEditEntryCategory(u, category) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может изменять составляющие программы")
		return false
	}
	return true
}

// touchEntry отмечает изменение записи: составляющие входят в основание отчёта,
// поэтому после их правки отчёт возвращается на проверку так же, как после
// правки самой записи.
func touchEntry(r *http.Request, tx *sql.Tx, entryID, userID string) error {
	_, err := tx.ExecContext(r.Context(), `UPDATE entries SET updated_at=now(),updated_by=$2::uuid WHERE id::text=$1`, entryID, userID)
	return err
}

// List отдаёт строки записи со сводкой.
func (h *TopItemHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !h.loadTopEntry(w, r, u, entryID, false) {
		return
	}
	items, err := topit.List(r.Context(), h.DB, entryID)
	if err != nil {
		writeTopError(w, err)
		return
	}
	summary, err := topit.Summarize(items)
	if err != nil {
		writeTopError(w, err)
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "summary": summary})
}

// Create добавляет строку.
func (h *TopItemHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !h.loadTopEntry(w, r, u, entryID, true) {
		return
	}
	var req topItemRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	in, err := topit.Normalize(topit.Input(req), curators.Today(time.Now()))
	if err != nil {
		writeTopError(w, err)
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	item, err := topit.Create(r.Context(), tx, entryID, in, u.ID)
	if err != nil {
		writeTopError(w, err)
		return
	}
	if touchEntry(r, tx, entryID, u.ID) != nil || logAudit(r.Context(), tx, "top_program_item", item.ID, "create", u.ID, "", nil, item) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, item)
}

// itemAndEntry читает строку и проверяет доступ к её записи.
func (h *TopItemHandlers) itemAndEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) (topit.Item, bool) {
	item, err := topit.Get(r.Context(), h.DB, id)
	if err != nil {
		writeTopError(w, err)
		return item, false
	}
	if !h.loadTopEntry(w, r, u, item.EntryID, true) {
		return item, false
	}
	return item, true
}

// Update заменяет содержимое строки.
func (h *TopItemHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	before, ok := h.itemAndEntry(w, r, u, id)
	if !ok {
		return
	}
	var req topItemRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	in, err := topit.Normalize(topit.Input(req), curators.Today(time.Now()))
	if err != nil {
		writeTopError(w, err)
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	after, err := topit.Update(r.Context(), tx, id, in)
	if err != nil {
		writeTopError(w, err)
		return
	}
	if touchEntry(r, tx, before.EntryID, u.ID) != nil || logAudit(r.Context(), tx, "top_program_item", id, "update", u.ID, "", before, after) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, after)
}

// Delete удаляет строку.
func (h *TopItemHandlers) Delete(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	before, ok := h.itemAndEntry(w, r, u, id)
	if !ok {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if err := topit.Delete(r.Context(), tx, id); err != nil {
		writeTopError(w, err)
		return
	}
	if touchEntry(r, tx, before.EntryID, u.ID) != nil || logAudit(r.Context(), tx, "top_program_item", id, "delete", u.ID, "", before, nil) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
