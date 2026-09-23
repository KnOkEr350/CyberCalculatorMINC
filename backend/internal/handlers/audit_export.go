package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
)

// auditExportRecord — одна строка выгрузки. Звенья цепи входят в выгрузку,
// поэтому получатель может перепроверить целостность сам, не доверяя ответу
// сервера на слово.
type auditExportRecord struct {
	ChainSeq  int64           `json:"chain_seq"`
	ID        string          `json:"id"`
	ActorType string          `json:"actor_type"`
	UserID    *string         `json:"user_id,omitempty"`
	Action    string          `json:"action"`
	Entity    string          `json:"entity_type"`
	EntityID  *string         `json:"entity_id,omitempty"`
	Old       json.RawMessage `json:"old_value,omitempty"`
	New       json.RawMessage `json:"new_value,omitempty"`
	RequestID string          `json:"request_id"`
	Comment   *string         `json:"comment,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	Archived  bool            `json:"archived"`
	PrevHash  *string         `json:"prev_hash"`
	RowHash   string          `json:"row_hash"`
}

// AUDIT-04: потоковая выгрузка журнала. Записи отдаются построчно (NDJSON) и
// не собираются в памяти целиком, поэтому объём выгрузки не ограничен размером
// ответа. Выгрузка начинается только после проверки цепочки хешей: отдавать
// журнал, целостность которого не подтверждена, бессмысленно.
func (h *AdminHandlers) ExportAuditLog(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	var brokenSeq sql.NullInt64
	var problem sql.NullString
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT chain_seq,problem FROM verify_audit_chain()`).Scan(&brokenSeq, &problem); err != nil && err != sql.ErrNoRows {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить целостность журнала")
		return
	}
	if problem.Valid {
		middleware.WriteError(w, http.StatusConflict,
			fmt.Sprintf("целостность журнала нарушена на звене %d: %s", brokenSeq.Int64, problem.String))
		return
	}

	q := r.URL.Query()
	conds := []string{"1=1"}
	args := []interface{}{}
	arg := func(v interface{}) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if v := strings.TrimSpace(q.Get("entity_type")); v != "" {
		conds = append(conds, "entity_type = "+arg(v))
	}
	if v := strings.TrimSpace(q.Get("user_id")); v != "" {
		conds = append(conds, "user_id = "+arg(v)+"::uuid")
	}
	if v := strings.TrimSpace(q.Get("request_id")); v != "" {
		conds = append(conds, "request_id = "+arg(v))
	}
	if v := strings.TrimSpace(q.Get("action")); v != "" {
		conds = append(conds, "action = "+arg(v))
	}
	// Границы периода задаются двумя отдельными условиями: текст SQL остаётся
	// постоянным, в запрос уходит только значение даты.
	day := func(name string) (time.Time, bool, bool) {
		value := strings.TrimSpace(q.Get(name))
		if value == "" {
			return time.Time{}, false, true
		}
		moment, err := time.Parse("2006-01-02", value)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "даты указываются как ГГГГ-ММ-ДД")
			return time.Time{}, false, false
		}
		return moment, true, true
	}
	from, hasFrom, ok := day("from")
	if !ok {
		return
	}
	if hasFrom {
		conds = append(conds, "created_at >= "+arg(from))
	}
	to, hasTo, ok := day("to")
	if !ok {
		return
	}
	if hasTo {
		conds = append(conds, "created_at < "+arg(to))
	}
	filter := strings.Join(conds, " AND ")

	// Структура запроса собирается только из фиксированных фрагментов, значения
	// остаются позиционными параметрами.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT chain_seq,id::text,actor_type,user_id::text,action,entity_type,entity_id::text,
		old_value,new_value,request_id,comment_text,created_at,prev_hash,row_hash,true
		FROM audit_log_archive WHERE ` + filter + `
		UNION ALL
		SELECT chain_seq,id::text,actor_type,user_id::text,action,entity_type,entity_id::text,
		old_value,new_value,request_id,comment_text,created_at,prev_hash,row_hash,false
		FROM audit_log WHERE ` + filter + `
		ORDER BY 1`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	// Заголовки ставятся до первой записи: после начала потока сообщить об
	// ошибке кодом ответа уже нельзя, поэтому обрыв виден по отсутствию
	// завершающей строки.
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.ndjson"`)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)

	exported := 0
	lastHash := ""
	for rows.Next() {
		var record auditExportRecord
		var userID, entityID, comment, prevHash sql.NullString
		var oldRaw, newRaw []byte
		if err := rows.Scan(&record.ChainSeq, &record.ID, &record.ActorType, &userID, &record.Action,
			&record.Entity, &entityID, &oldRaw, &newRaw, &record.RequestID, &comment,
			&record.CreatedAt, &prevHash, &record.RowHash, &record.Archived); err != nil {
			return
		}
		if userID.Valid {
			record.UserID = &userID.String
		}
		if entityID.Valid {
			record.EntityID = &entityID.String
		}
		if comment.Valid {
			record.Comment = &comment.String
		}
		if prevHash.Valid {
			record.PrevHash = &prevHash.String
		}
		record.Old, record.New = json.RawMessage(oldRaw), json.RawMessage(newRaw)
		if encoder.Encode(record) != nil {
			return
		}
		exported++
		lastHash = record.RowHash
		if flusher != nil && exported%200 == 0 {
			flusher.Flush()
		}
	}
	if rows.Err() != nil {
		return
	}
	// Завершающая строка отделена от записей: её наличие подтверждает, что
	// поток не оборвался на середине.
	_ = encoder.Encode(map[string]interface{}{
		"summary":        true,
		"exported":       exported,
		"last_row_hash":  lastHash,
		"chain_verified": true,
		"generated_at":   time.Now().UTC(),
	})
	if flusher != nil {
		flusher.Flush()
	}
}
