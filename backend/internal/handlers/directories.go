package handlers

import (
	"cybercalc/internal/middleware"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
)

func (h *EntryHandlers) validateMentor(r *http.Request, category, partner string, payload map[string]interface{}) error {
	if category != "internship" && category != "employment_practice" {
		return nil
	}
	id, _ := payload["mentor_id"].(string)
	if id == "" {
		// Compatibility for existing clients: only an already registered mentor
		// may be resolved by exact name. No implicit directory creation.
		name, _ := payload["mentor_full_name"].(string)
		if h.DB.QueryRowContext(r.Context(), `SELECT id FROM mentors WHERE partner_id::text=$1 AND lower(full_name)=lower($2)`, partner, strings.Join(strings.Fields(name), " ")).Scan(&id) != nil {
			return fmt.Errorf("выберите наставника из справочника этого партнёра")
		}
		payload["mentor_id"] = id
	}
	var name string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT full_name FROM mentors WHERE id::text=$1 AND partner_id::text=$2`, id, partner).Scan(&name); err != nil {
		return fmt.Errorf("выберите наставника из справочника этого партнёра")
	}
	// Snapshot used in reports: callers cannot forge a mentor's full name.
	payload["mentor_full_name"] = name
	return nil
}

func (h *EntryHandlers) Mentors(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	partner := r.URL.Query().Get("partner_id")
	if partner == "" {
		middleware.WriteError(w, 400, "сначала выберите партнёра")
		return
	}
	if !requirePartner(w, u, partner) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,full_name FROM mentors WHERE partner_id::text=$1 ORDER BY full_name,id`+page, partner)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		out = append(out, map[string]string{"id": id, "full_name": name})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения")
		return
	}
	writePage(w, r, out)
}

func validMentorName(name string) bool {
	if len([]rune(name)) < 2 || len([]rune(name)) > 200 || len(strings.Fields(name)) < 2 {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '\'' && r != '’' {
			return false
		}
	}
	return true
}

func (h *EntryHandlers) CreateMentor(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req struct {
		PartnerID string `json:"partner_id"`
		FullName  string `json:"full_name"`
	}
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req.FullName = strings.Join(strings.Fields(req.FullName), " ")
	if !requirePartner(w, u, req.PartnerID) {
		return
	}
	if !validMentorName(req.FullName) {
		middleware.WriteError(w, 400, "укажите полное имя наставника: фамилию, имя и отчество при наличии (до 200 символов)")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(r.Context(), `INSERT INTO mentors(partner_id,full_name) VALUES($1,$2) RETURNING id`, req.PartnerID, req.FullName).Scan(&id); err != nil {
		middleware.WriteError(w, 409, "наставник уже есть в справочнике или партнёр не найден")
		return
	}
	if logAudit(tx, "mentor", id, "create", u.ID, "", nil, req) != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id, "full_name": req.FullName})
}

func (h *PartnerHandlers) Directory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	if len([]rune(q.Get("q"))) > 200 {
		middleware.WriteError(w, 400, "поисковый запрос не должен превышать 200 символов")
		return
	}
	if kind := q.Get("partner_kind"); kind != "" && kind != "vuz" && kind != "kolledj" && kind != "school" {
		middleware.WriteError(w, 400, "некорректный тип организации")
		return
	}
	verifiedOnly := q.Get("verified_only")
	if verifiedOnly != "" && verifiedOnly != "0" && verifiedOnly != "1" {
		middleware.WriteError(w, 400, "verified_only должен быть 0 или 1")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,name,partner_kind,region,source,
		COALESCE(inn,''),COALESCE(ogrn,''),COALESCE(license_number,''),license_status,institution_status,
		COALESCE(registry_record_id,''),COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),
		COALESCE(verified_at::text,''),verification_status,
		(verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days')
		FROM education_directory
		WHERE ($1='' OR partner_kind=$1)
		AND ($2='' OR name ILIKE '%'||$2||'%' OR region ILIKE '%'||$2||'%' OR inn=$2 OR ogrn=$2 OR license_number ILIKE '%'||$2||'%')
		AND ($3<>'1' OR (verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days'))
		ORDER BY (verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days') DESC,name LIMIT 100`, q.Get("partner_kind"), strings.TrimSpace(q.Get("q")), verifiedOnly)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name, kind, region, source, inn, ogrn, licenseNumber, licenseStatus, institutionStatus string
		var recordID, sourceURL, registryUpdatedAt, verifiedAt, verificationStatus string
		var selectable bool
		if rows.Scan(&id, &name, &kind, &region, &source, &inn, &ogrn, &licenseNumber, &licenseStatus,
			&institutionStatus, &recordID, &sourceURL, &registryUpdatedAt, &verifiedAt, &verificationStatus, &selectable) != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "name": name, "partner_kind": kind, "region": region, "source": source,
			"inn": inn, "ogrn": ogrn, "license_number": licenseNumber, "license_status": licenseStatus,
			"institution_status": institutionStatus, "registry_record_id": recordID, "source_url": sourceURL,
			"registry_updated_at": registryUpdatedAt, "verified_at": verifiedAt, "verification_status": verificationStatus,
			"selectable": selectable,
		})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения")
		return
	}
	middleware.WriteJSON(w, 200, out)
}

func (h *PartnerHandlers) DirectoryStats(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var total, verified, universities, colleges, schools int
	var lastVerified sql.NullTime
	err := h.DB.QueryRowContext(r.Context(), `SELECT count(*),
		count(*) FILTER(WHERE verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days'),
		count(*) FILTER(WHERE partner_kind='vuz' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days'),
		count(*) FILTER(WHERE partner_kind='kolledj' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days'),
		count(*) FILTER(WHERE partner_kind='school' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days'),
		max(verified_at) FROM education_directory`).Scan(&total, &verified, &universities, &colleges, &schools, &lastVerified)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка статистики справочника")
		return
	}
	last := ""
	if lastVerified.Valid {
		last = lastVerified.Time.Format(time.RFC3339)
	}
	var syncStatus, syncSource, syncError string
	var syncFinished sql.NullTime
	err = h.DB.QueryRowContext(r.Context(), `SELECT status,source_url,COALESCE(error_text,''),finished_at
		FROM directory_sync_runs ORDER BY started_at DESC,id DESC LIMIT 1`).Scan(&syncStatus, &syncSource, &syncError, &syncFinished)
	if err != nil && err != sql.ErrNoRows {
		middleware.WriteError(w, 500, "ошибка состояния автообновления")
		return
	}
	syncFinishedAt := ""
	if syncFinished.Valid {
		syncFinishedAt = syncFinished.Time.Format(time.RFC3339)
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{
		"total": total, "verified_active": verified, "universities": universities,
		"colleges": colleges, "schools": schools, "last_verified_at": last,
		"sync_status": syncStatus, "sync_source": syncSource, "sync_error": syncError, "sync_finished_at": syncFinishedAt,
	})
}
