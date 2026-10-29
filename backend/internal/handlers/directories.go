package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/lib/pq"
)

func (h *EntryHandlers) validateMentor(r *http.Request, category, periodType, partner, excludeEntryID string, payload map[string]interface{}) error {
	if category != "internship" && category != "employment_practice" {
		return nil
	}
	id, _ := payload["mentor_id"].(string)
	if id == "" {
		// Compatibility for existing clients: only an already registered mentor
		// may be resolved by exact name. No implicit directory creation.
		name, _ := payload["mentor_full_name"].(string)
		if h.DB.QueryRowContext(r.Context(), `SELECT id FROM mentors WHERE partner_id::text=$1 AND lower(full_name)=lower($2)
			ORDER BY archived_at IS NOT NULL LIMIT 1`, partner, strings.Join(strings.Fields(name), " ")).Scan(&id) != nil {
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
	// Отсутствующее поле - пустая строка, а не "<nil>": fmt.Sprint(nil) выглядел
	// бы заполненным значением и уводил проверку в SQL с некорректной датой.
	text := func(key string) string {
		value, _ := payload[key].(string)
		return strings.TrimSpace(value)
	}
	student := strings.Join(strings.Fields(text("student_full_name")), " ")
	start := text("mentor_assignment_start")
	end := text("mentor_assignment_end")
	if student == "" || start == "" || end == "" {
		return nil // the category validator returns the field-specific message
	}
	// Даты проверяются здесь, до запроса: иначе PostgreSQL отвечает общей
	// ошибкой приведения типа, и пользователь не понимает, что исправить.
	startDate, startErr := time.Parse("2006-01-02", start)
	endDate, endErr := time.Parse("2006-01-02", end)
	if startErr != nil || endErr != nil {
		return fmt.Errorf("даты назначения наставника укажите в формате ГГГГ-ММ-ДД и проверьте год")
	}
	if endDate.Before(startDate) {
		return fmt.Errorf("дата окончания назначения наставника не может быть раньше даты начала")
	}
	var overlap bool
	if err := h.DB.QueryRowContext(r.Context(), `SELECT EXISTS(
		SELECT 1 FROM entries
		WHERE mentor_id::text=$1 AND lower(assigned_student_name)=lower($2)
		  AND daterange(mentor_assignment_start,mentor_assignment_end,'[]') && daterange($3::date,$4::date,'[]')
		  AND period_type=$6
		  AND ($5='' OR id::text<>$5)
	)`, id, student, start, end, excludeEntryID, periodType).Scan(&overlap); err != nil {
		return fmt.Errorf("не удалось проверить период назначения наставника")
	}
	if overlap {
		return fmt.Errorf("у этого наставника уже есть пересекающееся назначение для того же студента")
	}
	return nil
}

// Mentors lists one partner's mentors for the entry forms, or, without
// partner_id, every mentor of the caller's IT company for the settings list.
func (h *EntryHandlers) Mentors(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	partner := r.URL.Query().Get("partner_id")
	includeArchived := r.URL.Query().Get("include_archived") == "1"
	company := ""
	if partner == "" {
		if !isStaff(u) || u.ITCompanyID == nil || *u.ITCompanyID == "" {
			middleware.WriteError(w, 400, "сначала выберите партнёра")
			return
		}
		company = *u.ITCompanyID
	} else if !requirePartnerTenant(w, r, h.DB, u, partner) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT m.id,m.partner_id,p.name,p.partner_kind,m.full_name,m.last_name,m.first_name,m.middle_name,
		m.position,m.archived_at IS NOT NULL
		FROM mentors m JOIN partners p ON p.id=m.partner_id
		WHERE ($1='' OR m.partner_id::text=$1) AND ($2='' OR p.it_company_id::text=$2) AND ($3 OR m.archived_at IS NULL)
		ORDER BY m.full_name,m.id`+page, partner, company, includeArchived)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, partnerID, partnerName, partnerKind, name, last, first, middle, position string
		var archived bool
		if err := rows.Scan(&id, &partnerID, &partnerName, &partnerKind, &name, &last, &first, &middle, &position, &archived); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		out = append(out, map[string]interface{}{"id": id, "partner_id": partnerID, "partner_name": partnerName, "partner_kind": partnerKind,
			"full_name": name, "last_name": last, "first_name": first, "middle_name": middle, "position": position, "archived": archived})
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

type createMentorRequest struct {
	PartnerID  string `json:"partner_id"`
	FullName   string `json:"full_name"`
	LastName   string `json:"last_name"`
	FirstName  string `json:"first_name"`
	MiddleName string `json:"middle_name"`
	Position   string `json:"position"`
}

// normalizeMentor accepts either the structured name (current form) or the
// legacy single full_name, and always derives full_name from the parts.
func normalizeMentor(req *createMentorRequest) string {
	clean := func(value string) string { return strings.Join(strings.Fields(value), " ") }
	req.LastName, req.FirstName, req.MiddleName = clean(req.LastName), clean(req.FirstName), clean(req.MiddleName)
	req.Position = clean(req.Position)
	if req.LastName == "" && req.FirstName == "" && req.MiddleName == "" {
		parts := strings.Fields(req.FullName)
		if len(parts) >= 2 {
			req.LastName, req.FirstName, req.MiddleName = parts[0], parts[1], strings.Join(parts[2:], " ")
		}
	} else if req.LastName == "" || req.FirstName == "" {
		return "укажите фамилию и имя наставника"
	}
	req.FullName = strings.TrimSpace(strings.Join([]string{req.LastName, req.FirstName, req.MiddleName}, " "))
	if !validMentorName(req.FullName) || len([]rune(req.LastName)) > 100 || len([]rune(req.FirstName)) > 100 || len([]rune(req.MiddleName)) > 100 {
		return "укажите полное имя наставника: фамилию, имя и отчество при наличии (до 200 символов)"
	}
	if len([]rune(req.Position)) > 200 {
		return "должность наставника: не более 200 символов"
	}
	return ""
}

func (h *EntryHandlers) CreateMentor(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req createMentorRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, req.PartnerID) {
		return
	}
	if message := normalizeMentor(&req); message != "" {
		middleware.WriteError(w, 400, message)
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	if err := tx.QueryRowContext(r.Context(), `INSERT INTO mentors(partner_id,full_name,last_name,first_name,middle_name,position)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, req.PartnerID, req.FullName, req.LastName, req.FirstName, req.MiddleName, req.Position).Scan(&id); err != nil {
		middleware.WriteError(w, 409, "наставник уже есть в справочнике или партнёр не найден")
		return
	}
	if logAudit(r.Context(), tx, "mentor", id, "create", u.ID, "", nil, req) != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id, "full_name": req.FullName, "position": req.Position})
}

func (h *EntryHandlers) mentorPartner(r *http.Request, id string) (string, error) {
	var partnerID string
	err := h.DB.QueryRowContext(r.Context(), `SELECT partner_id FROM mentors WHERE id::text=$1 AND archived_at IS NULL`, id).Scan(&partnerID)
	return partnerID, err
}

// UpdateMentor changes the directory card only; entries keep the mentor name
// snapshot that was valid when they were saved.
func (h *EntryHandlers) UpdateMentor(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	partnerID, err := h.mentorPartner(r, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "наставник не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения наставника")
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	var req createMentorRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	if message := normalizeMentor(&req); message != "" {
		middleware.WriteError(w, 400, message)
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `UPDATE mentors SET full_name=$2,last_name=$3,first_name=$4,middle_name=$5,position=$6,updated_at=now()
		WHERE id::text=$1`, id, req.FullName, req.LastName, req.FirstName, req.MiddleName, req.Position); err != nil {
		middleware.WriteError(w, 409, "наставник с таким ФИО уже есть у этого партнёра")
		return
	}
	if logAudit(r.Context(), tx, "mentor", id, "update", u.ID, "", nil, req) != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": id, "full_name": req.FullName, "position": req.Position})
}

// DeleteMentor archives a mentor who is already referenced by activities, so
// reports and assignment history stay intact; an unused card is removed.
func (h *EntryHandlers) DeleteMentor(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if u.Role != models.RoleSuperAdmin || u.EntityType == models.EntityEduInst {
		middleware.WriteError(w, http.StatusForbidden, "удалять наставников может только системный администратор")
		return
	}
	partnerID, err := h.mentorPartner(r, id)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "наставник не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения наставника")
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, partnerID) {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var used bool
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM entries WHERE mentor_id::text=$1 OR payload->>'mentor_id'=$1)
		OR EXISTS(SELECT 1 FROM practice_records WHERE mentor_id::text=$1)`, id).Scan(&used); err != nil {
		middleware.WriteError(w, 500, "не удалось проверить использование наставника")
		return
	}
	mode := "deleted"
	if used {
		mode = "archived"
		_, err = tx.ExecContext(r.Context(), `UPDATE mentors SET archived_at=now(),updated_at=now() WHERE id::text=$1`, id)
	} else {
		_, err = tx.ExecContext(r.Context(), `DELETE FROM mentors WHERE id::text=$1`, id)
	}
	if err != nil {
		middleware.WriteError(w, 409, "не удалось удалить наставника")
		return
	}
	if logAudit(r.Context(), tx, "mentor", id, "delete", u.ID, "", nil, map[string]string{"mode": mode}) != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка удаления")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": id, "result": mode})
}

func (h *PartnerHandlers) Directory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canReviewEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к справочнику учебных заведений")
		return
	}
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
	// The review queue remains reachable explicitly; normal lists contain only
	// universities with at least one exact program code from Order 27.
	reviewAll := q.Get("review_all") == "1"
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	if verifiedOnly != "" && verifiedOnly != "0" && verifiedOnly != "1" {
		middleware.WriteError(w, 400, "verified_only должен быть 0 или 1")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT d.id,d.name,d.partner_kind,d.region,d.source,
		COALESCE(d.inn,''),COALESCE(d.ogrn,''),COALESCE(d.license_number,''),d.license_status,d.institution_status,
		COALESCE(d.registry_record_id,''),COALESCE(d.source_url,''),COALESCE(d.registry_updated_at::text,''),
		COALESCE(d.verified_at::text,''),d.verification_status,COALESCE(d.verified_by::text,''),
		COALESCE(verifier.full_name,''),COALESCE(verifier.email,''),d.program_codes,d.programs_source_url,
		ARRAY(SELECT code FROM unnest(d.program_codes) AS code WHERE education_matches_order('vuz',ARRAY[code])),
		education_matches_order(d.partner_kind,d.program_codes),d.listed_in_mincifry_order_27,
		(d.verification_status='verified' AND d.license_status='active' AND d.institution_status='active'
		 AND education_matches_order(d.partner_kind,d.program_codes)
		 AND (d.partner_kind<>'vuz' OR d.listed_in_mincifry_order_27)
		 AND d.verified_at>=now()-interval '35 days' AND d.registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE)
		FROM education_directory d LEFT JOIN users verifier ON verifier.id=d.verified_by
		WHERE ($1='' OR d.partner_kind=$1)
		AND ($4 OR education_matches_order(d.partner_kind,d.program_codes))
		AND ($4 OR d.partner_kind<>'vuz' OR d.listed_in_mincifry_order_27)
		AND ($4 OR d.verification_status<>'rejected')
		AND ($2='' OR d.name ILIKE '%'||$2||'%' OR d.region ILIKE '%'||$2||'%' OR d.inn=$2 OR d.ogrn=$2 OR d.license_number ILIKE '%'||$2||'%')
		AND ($3<>'1' OR (d.verification_status='verified' AND d.license_status='active' AND d.institution_status='active'
		 AND education_matches_order(d.partner_kind,d.program_codes)
		 AND d.verified_at>=now()-interval '35 days' AND d.registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE))
		ORDER BY (d.verification_status='verified' AND d.license_status='active' AND d.institution_status='active'
		 AND d.verified_at>=now()-interval '35 days' AND d.registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE) DESC,d.name,d.id`+page, q.Get("partner_kind"), strings.TrimSpace(q.Get("q")), verifiedOnly, reviewAll)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name, kind, region, source, inn, ogrn, licenseNumber, licenseStatus, institutionStatus string
		var recordID, sourceURL, registryUpdatedAt, verifiedAt, verificationStatus string
		var verifiedBy, verifierName, verifierEmail string
		var programCodes pq.StringArray
		var matchingCodes pq.StringArray
		var programsSource string
		var matchesOrder, listedInOrder bool
		var selectable bool
		if rows.Scan(&id, &name, &kind, &region, &source, &inn, &ogrn, &licenseNumber, &licenseStatus,
			&institutionStatus, &recordID, &sourceURL, &registryUpdatedAt, &verifiedAt, &verificationStatus,
			&verifiedBy, &verifierName, &verifierEmail, &programCodes, &programsSource, &matchingCodes, &matchesOrder, &listedInOrder, &selectable) != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		out = append(out, map[string]interface{}{
			"id": id, "name": name, "partner_kind": kind, "region": region, "source": source,
			"inn": inn, "ogrn": ogrn, "license_number": licenseNumber, "license_status": licenseStatus,
			"institution_status": institutionStatus, "registry_record_id": recordID, "source_url": sourceURL,
			"registry_updated_at": registryUpdatedAt, "verified_at": verifiedAt, "verification_status": verificationStatus,
			"verified_by": verifiedBy, "verifier_name": verifierName, "verifier_email": verifierEmail,
			"selectable":    selectable,
			"program_codes": programCodes, "programs_source_url": programsSource, "matches_order": matchesOrder,
			"matching_program_codes": matchingCodes, "listed_in_mincifry_order_27": listedInOrder,
		})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения")
		return
	}
	writePage(w, r, out)
}

func (h *PartnerHandlers) DirectoryStats(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canReviewEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к справочнику учебных заведений")
		return
	}
	var total, verified, pending, universities, colleges, schools int
	var lastVerified sql.NullTime
	err := h.DB.QueryRowContext(r.Context(), `SELECT count(*),
		count(*) FILTER(WHERE verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days' AND registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE),
		count(*) FILTER(WHERE verification_status='pending'),
		count(*) FILTER(WHERE partner_kind='vuz' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days' AND registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE),
		count(*) FILTER(WHERE partner_kind='kolledj' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days' AND registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE),
		count(*) FILTER(WHERE partner_kind='school' AND verification_status='verified' AND license_status='active' AND institution_status='active' AND verified_at>=now()-interval '35 days' AND registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE),
		max(verified_at) FROM education_directory
		WHERE verification_status<>'rejected'
		AND education_matches_order(partner_kind,program_codes)
		AND (partner_kind<>'vuz' OR listed_in_mincifry_order_27)`).Scan(&total, &verified, &pending, &universities, &colleges, &schools, &lastVerified)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка статистики справочника")
		return
	}
	var allTotal, allUniversities, programsUnknown int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT count(*),count(*) FILTER(WHERE partner_kind='vuz'),
		count(*) FILTER(WHERE partner_kind='vuz' AND programs_checked_at IS NULL) FROM education_directory
		WHERE partner_kind='vuz' AND verification_status<>'rejected' AND listed_in_mincifry_order_27`).Scan(&allTotal, &allUniversities, &programsUnknown); err != nil {
		middleware.WriteError(w, 500, "ошибка статистики направлений")
		return
	}
	var pendingProposals, ownPendingProposals int
	if err := h.DB.QueryRowContext(r.Context(), `SELECT
		count(*) FILTER(WHERE proposed_by IS NOT NULL AND verification_status='pending'),
		count(*) FILTER(WHERE proposed_by::text=$1 AND verification_status='pending')
		FROM education_directory`, u.ID).Scan(&pendingProposals, &ownPendingProposals); err != nil {
		middleware.WriteError(w, 500, "ошибка статистики предложений")
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
		"total": total, "verified_active": verified, "pending": pending, "universities": universities,
		"all_total": allTotal, "all_universities": allUniversities, "programs_unknown": programsUnknown,
		"pending_proposals": pendingProposals, "own_pending_proposals": ownPendingProposals,
		"colleges": colleges, "schools": schools, "last_verified_at": last,
		"sync_status": syncStatus, "sync_source": syncSource, "sync_error": syncError, "sync_finished_at": syncFinishedAt,
	})
}
