package handlers

import (
	"net/http"
	"strconv"

	"cybercalc/internal/middleware"
)

type legacyFinding struct {
	ID             int64  `json:"id"`
	EntryID        string `json:"entry_id"`
	CategoryCode   string `json:"category_code"`
	RuleCode       string `json:"rule_code"`
	Detail         string `json:"detail"`
	SuggestedValue string `json:"suggested_value,omitempty"`
	DetectedAt     string `json:"detected_at"`
}

// LegacyFindings отдаёт записи, помеченные дозаполнением на ручное уточнение.
// Список ограничен арендатором: пометки чужой ИТ-компании не показываются.
func (h *AdminHandlers) LegacyFindings(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	company := itCompanyScope(admin)
	limit := 200
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT f.id,f.entry_id::text,f.category_code,f.rule_code,f.detail,
			COALESCE(f.suggested_value,''),f.detected_at::text
		FROM legacy_backfill_findings f JOIN entries e ON e.id=f.entry_id
		WHERE f.resolved_at IS NULL AND ($1='' OR e.it_company_id::text=$1)
		  AND ($2='' OR f.category_code=$2)
		ORDER BY f.detected_at,f.id LIMIT $3`, company, r.URL.Query().Get("category_code"), limit)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения пометок")
		return
	}
	defer rows.Close()
	out := make([]legacyFinding, 0)
	for rows.Next() {
		var item legacyFinding
		if err := rows.Scan(&item.ID, &item.EntryID, &item.CategoryCode, &item.RuleCode, &item.Detail,
			&item.SuggestedValue, &item.DetectedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения пометок")
			return
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения пометок")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

type directoryFinding struct {
	ID         int64  `json:"id"`
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	RuleCode   string `json:"rule_code"`
	Detail     string `json:"detail"`
	DetectedAt string `json:"detected_at"`
}

// LegacyDirectoryFindings отдаёт строки справочников — партнёров, соглашений,
// ИТ-компании, — перенесённые с пробелами или неоднозначные (DATA-13).
// Арендатор видит только свои находки.
func (h *AdminHandlers) LegacyDirectoryFindings(w http.ResponseWriter, r *http.Request, admin middleware.AuthUser) {
	limit := 200
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,entity_type,entity_id::text,rule_code,detail,detected_at::text
		FROM directory_backfill_findings
		WHERE resolved_at IS NULL AND ($1='' OR it_company_id::text=$1)
		  AND ($2='' OR entity_type=$2) AND ($3='' OR rule_code=$3)
		ORDER BY entity_type,rule_code,detected_at,id LIMIT $4`,
		itCompanyScope(admin), r.URL.Query().Get("entity_type"), r.URL.Query().Get("rule_code"), limit)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения находок справочников")
		return
	}
	defer rows.Close()
	out := make([]directoryFinding, 0)
	for rows.Next() {
		var item directoryFinding
		if err := rows.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.RuleCode, &item.Detail, &item.DetectedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения находок справочников")
			return
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения находок справочников")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}
