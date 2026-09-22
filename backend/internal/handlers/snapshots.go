package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type SnapshotHandlers struct{ DB *sql.DB }

type snapshotEntry struct {
	ID            string          `json:"id"`
	CategoryCode  string          `json:"category_code"`
	PartnerID     *string         `json:"partner_id"`
	PartnerName   string          `json:"partner_name"`
	AgreementID   *string         `json:"agreement_id"`
	AgreementNo   string          `json:"agreement_number"`
	PeriodType    string          `json:"period_type"`
	Audience      string          `json:"audience"`
	Payload       json.RawMessage `json:"payload"`
	AmountRub     string          `json:"amount_rub"`
	FormulaAmount string          `json:"formula_amount_rub"`
	ActualAmount  *string         `json:"actual_amount_rub"`
	CostMethod    string          `json:"cost_method"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Eligible      bool            `json:"eligible"`
	Readiness     string          `json:"readiness"`
}

type snapshotDocument struct {
	ID            string     `json:"id"`
	EntryID       string     `json:"entry_id"`
	FileName      string     `json:"file_name"`
	ContentType   string     `json:"content_type"`
	SizeBytes     int64      `json:"size_bytes"`
	ContentSHA256 string     `json:"content_sha256,omitempty"`
	DocumentType  string     `json:"document_type"`
	ReviewStatus  string     `json:"review_status"`
	ReviewedAt    *time.Time `json:"reviewed_at"`
	UploadedAt    time.Time  `json:"uploaded_at"`
}

type snapshotPayload struct {
	SchemaVersion int                `json:"schema_version"`
	CompanyID     string             `json:"it_company_id"`
	ReportYear    int                `json:"report_year"`
	SnapshotDate  string             `json:"snapshot_date"`
	Entries       []snapshotEntry    `json:"entries"`
	Documents     []snapshotDocument `json:"documents"`
}

func moscowDate(now time.Time) (int, time.Month, int) {
	return now.In(time.FixedZone("Europe/Moscow", 3*60*60)).Date()
}

func maySnapshotAllowed(now time.Time, reportYear int) bool {
	year, month, day := moscowDate(now)
	return year == reportYear && month == time.May && day == 1
}

func snapshotCompany(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) (string, bool) {
	if u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin {
		companyID := r.URL.Query().Get("it_company_id")
		if companyID != "" {
			return companyID, true
		}
	}
	if u.ITCompanyID != nil && *u.ITCompanyID != "" {
		return *u.ITCompanyID, true
	}
	middleware.WriteError(w, http.StatusBadRequest, "укажите ИТ-компанию для снимка")
	return "", false
}

func (h *SnapshotHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if u.Role != models.RoleSuperAdmin && u.Role != models.RoleHoldingAdmin && u.Role != models.RoleOrgAdmin {
		middleware.WriteError(w, http.StatusForbidden, "роль не может формировать снимок")
		return
	}
	year, err := strconv.Atoi(r.URL.Query().Get("report_year"))
	if err != nil || year < 2000 || year > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите корректный отчётный год")
		return
	}
	if !maySnapshotAllowed(time.Now(), year) {
		middleware.WriteError(w, http.StatusConflict, "снимок факта формируется только 1 мая отчётного года по московскому времени")
		return
	}
	companyID, ok := snapshotCompany(w, r, u)
	if !ok {
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: false})
	if err != nil {
		middleware.WriteError(w, 500, "не удалось начать формирование снимка")
		return
	}
	defer tx.Rollback()

	payload := snapshotPayload{SchemaVersion: 1, CompanyID: companyID, ReportYear: year, SnapshotDate: fmt.Sprintf("%04d-05-01", year), Entries: []snapshotEntry{}, Documents: []snapshotDocument{}}
	rows, err := tx.QueryContext(r.Context(), `SELECT e.id::text,e.category_code,e.partner_id::text,e.agreement_id::text,COALESCE(p.name,''),COALESCE(a.number,''),e.period_type,e.audience,e.payload,
		e.amount_rub::text,e.formula_amount_rub::text,e.actual_amount_rub::text,e.cost_method,e.updated_at,eligibility.eligible
		FROM entries e LEFT JOIN partners p ON p.id=e.partner_id LEFT JOIN agreements a ON a.id=e.agreement_id
		JOIN entry_eligibility eligibility ON eligibility.id=e.id
		WHERE e.it_company_id::text=$1 AND e.report_year=$2 AND e.period_type='fact' ORDER BY e.id`, companyID, year)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать фактические мероприятия")
		return
	}
	for rows.Next() {
		var item snapshotEntry
		var partner, agreement, actual sql.NullString
		if err := rows.Scan(&item.ID, &item.CategoryCode, &partner, &agreement, &item.PartnerName, &item.AgreementNo, &item.PeriodType, &item.Audience, &item.Payload, &item.AmountRub, &item.FormulaAmount, &actual, &item.CostMethod, &item.UpdatedAt, &item.Eligible); err != nil {
			rows.Close()
			middleware.WriteError(w, 500, "не удалось прочитать фактические мероприятия")
			return
		}
		if partner.Valid {
			item.PartnerID = &partner.String
		}
		if agreement.Valid {
			item.AgreementID = &agreement.String
		}
		if actual.Valid {
			item.ActualAmount = &actual.String
		}
		payload.Entries = append(payload.Entries, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		middleware.WriteError(w, 500, "не удалось прочитать фактические мероприятия")
		return
	}
	rows.Close()

	docs, err := tx.QueryContext(r.Context(), `SELECT a.id::text,a.entry_id::text,a.file_name,COALESCE(a.content_type,''),a.size_bytes,COALESCE(a.content_sha256,''),
		a.document_type,a.review_status,a.reviewed_at,a.uploaded_at FROM attachments a JOIN entries e ON e.id=a.entry_id
		WHERE e.it_company_id::text=$1 AND e.report_year=$2 AND e.period_type='fact' AND a.retention_expires_at>now() ORDER BY a.id`, companyID, year)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать документы снимка")
		return
	}
	for docs.Next() {
		var item snapshotDocument
		var reviewed sql.NullTime
		if err := docs.Scan(&item.ID, &item.EntryID, &item.FileName, &item.ContentType, &item.SizeBytes, &item.ContentSHA256, &item.DocumentType, &item.ReviewStatus, &reviewed, &item.UploadedAt); err != nil {
			docs.Close()
			middleware.WriteError(w, 500, "не удалось прочитать документы снимка")
			return
		}
		if reviewed.Valid {
			item.ReviewedAt = &reviewed.Time
		}
		if item.ContentSHA256 == "" {
			docs.Close()
			middleware.WriteError(w, http.StatusConflict, "снимок не сформирован: повторно загрузите документы без контрольной суммы")
			return
		}
		payload.Documents = append(payload.Documents, item)
	}
	if err := docs.Err(); err != nil {
		docs.Close()
		middleware.WriteError(w, 500, "не удалось прочитать документы снимка")
		return
	}
	docs.Close()
	documentTokens := map[string][]string{}
	for _, document := range payload.Documents {
		documentTokens[document.EntryID] = append(documentTokens[document.EntryID], document.DocumentType+":"+document.ReviewStatus)
	}
	for index := range payload.Entries {
		entry := &payload.Entries[index]
		fields := map[string]interface{}{}
		if err := json.Unmarshal(entry.Payload, &fields); err != nil {
			middleware.WriteError(w, 500, "не удалось оценить готовность снимка")
			return
		}
		entry.Readiness = compliance.Evaluate(entry.CategoryCode, "fact", fields, documentTokens[entry.ID]).State
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сериализовать снимок")
		return
	}
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO report_snapshots(it_company_id,report_year,snapshot_date,payload,payload_bytes,payload_sha256,sealed_by)
		VALUES($1,$2,make_date($2,5,1),$3::jsonb,$4,$5,$6) RETURNING id::text`, companyID, year, string(encoded), encoded, hash, u.ID).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "неизменяемый снимок за этот год уже сформирован")
		return
	}
	if err := logAudit(r.Context(), tx, "report_snapshot", id, "seal", u.ID, "неизменяемый снимок факта на 1 мая", nil, map[string]interface{}{"report_year": year, "sha256": hash}); err != nil {
		middleware.WriteError(w, 500, "не удалось записать аудит снимка")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "не удалось зафиксировать снимок")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "report_year": year, "snapshot_date": payload.SnapshotDate, "sha256": hash, "entries_count": len(payload.Entries), "documents_count": len(payload.Documents)})
}

func (h *SnapshotHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	companyID, ok := snapshotCompany(w, r, u)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id::text,report_year,snapshot_date::text,captured_at,payload_sha256,sealed_by::text,
		jsonb_array_length(payload->'entries'),jsonb_array_length(payload->'documents') FROM report_snapshots
		WHERE it_company_id::text=$1 ORDER BY report_year DESC`, companyID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось загрузить снимки")
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var id, date, hash, sealedBy string
		var year, entries, documents int
		var captured time.Time
		if rows.Scan(&id, &year, &date, &captured, &hash, &sealedBy, &entries, &documents) != nil {
			middleware.WriteError(w, 500, "не удалось прочитать снимки")
			return
		}
		items = append(items, map[string]interface{}{"id": id, "report_year": year, "snapshot_date": date, "captured_at": captured, "sha256": hash, "sealed_by": sealedBy, "entries_count": entries, "documents_count": documents})
	}
	if err := rows.Err(); err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать снимки")
		return
	}
	middleware.WriteJSON(w, 200, items)
}

func (h *SnapshotHandlers) Download(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if u.Role != models.RoleSuperAdmin && u.Role != models.RoleHoldingAdmin && u.Role != models.RoleOrgAdmin && u.Role != models.RoleAuditorViewer {
		middleware.WriteError(w, http.StatusForbidden, "роль не может скачивать полный снимок организации")
		return
	}
	companyID, ok := snapshotCompany(w, r, u)
	if !ok {
		return
	}
	var payload []byte
	var expected string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT payload_bytes,payload_sha256 FROM report_snapshots WHERE id::text=$1 AND it_company_id::text=$2`, id, companyID).Scan(&payload, &expected); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "снимок не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать снимок")
		return
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != expected {
		middleware.WriteError(w, 500, "контрольная сумма снимка нарушена")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="report-snapshot-`+id+`.json"`)
	w.Header().Set("X-Content-SHA256", expected)
	w.Write(payload)
}
