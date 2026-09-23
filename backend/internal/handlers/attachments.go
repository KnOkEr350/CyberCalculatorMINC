package handlers

import (
	"crypto/sha256"
	"cybercalc/internal/compliance"
	"cybercalc/internal/filestore"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type AttachmentHandlers struct {
	DB             *sql.DB
	UploadDir      string
	ScannerAddress string
	// ScannerPolicy — что делать, если антивирус не ответил: "reject" (по
	// умолчанию) не принимает файл, "quarantine" принимает и держит его
	// недоступным до повторной проверки.
	ScannerPolicy string
	QuotaBytes    int64
}

const maxAttachmentSize int64 = 20 << 20
const maxAttachmentCount = 20

// Optional attachments for plan and fact. One batch is all-or-nothing.
func (h *AttachmentHandlers) Upload(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !canUploadAnyDocument(u) {
		middleware.WriteError(w, http.StatusForbidden, "роль не может загружать документы")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	documentType := strings.TrimSpace(r.URL.Query().Get("document_type"))
	if documentType == "" {
		documentType = "other"
	}
	if !compliance.ValidDocumentType(documentType) {
		middleware.WriteError(w, 400, "неизвестный тип подтверждающего документа")
		return
	}
	var category string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT category_code FROM entries WHERE id::text=$1`, entryID).Scan(&category); err != nil {
		middleware.WriteError(w, 404, "запись не найдена")
		return
	}
	if !canUploadDocument(u, category, documentType) {
		middleware.WriteError(w, 403, "роль не может загружать этот тип документа")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		middleware.WriteError(w, 400, "ожидаются файлы multipart/form-data, не более 20 МБ суммарно")
		return
	}
	defer r.MultipartForm.RemoveAll()
	headers := append(r.MultipartForm.File["files"], r.MultipartForm.File["file"]...)
	if len(headers) == 0 || len(headers) > maxAttachmentCount {
		middleware.WriteError(w, 400, "выберите от 1 до 20 файлов")
		return
	}
	var total int64
	for _, f := range headers {
		if f.Size <= 0 || f.Size > maxAttachmentSize || len([]rune(f.Filename)) > 255 {
			middleware.WriteError(w, 400, "пустой файл, недопустимый размер или слишком длинное имя")
			return
		}
		total += f.Size
	}
	if total > maxAttachmentSize {
		middleware.WriteError(w, 413, "суммарный размер превышает 20 МБ")
		return
	}
	type stagedFile struct {
		name, path, contentSHA256 string
		size                      int64
		// shared — байты уже лежали в хранилище до этой загрузки.
		shared bool
		// scanStatus — исход антивирусной проверки этого файла.
		scanStatus, scanSignature string
	}
	staged := make([]stagedFile, 0, len(headers))
	// Files are unreferenced until the short metadata transaction commits.
	// Do not hold a database connection or quota lock during scanning.
	commitAttempted := false
	defer func() {
		if !commitAttempted {
			for _, f := range staged {
				if f.shared {
					continue
				}
				if err := filestore.Remove(h.UploadDir, f.path); err != nil && !os.IsNotExist(err) {
					slog.Error("staged file cleanup failed", "entry_id", entryID)
				}
			}
		}
	}()
	for _, header := range headers {
		if r.Context().Err() != nil {
			middleware.WriteError(w, 408, "запрос отменён")
			return
		}
		src, err := header.Open()
		if err != nil {
			middleware.WriteError(w, 400, "не удалось прочитать файл")
			return
		}
		// STORE-01: файл адресуется своим SHA-256 и пишется потоково с
		// атомарным переименованием. Одинаковые байты занимают один blob,
		// поэтому повторная загрузка того же документа не удваивает хранилище.
		blob, blobErr := filestore.CreateBlob(h.UploadDir, src, maxAttachmentSize)
		src.Close()
		if blobErr != nil {
			middleware.WriteError(w, 500, "ошибка записи файла")
			return
		}
		path := blob.Path
		if blob.Size != header.Size {
			if !blob.Deduplicated {
				if err := filestore.Remove(h.UploadDir, path); err != nil && !os.IsNotExist(err) {
					slog.Error("staged blob cleanup failed", "entry_id", entryID)
				}
			}
			middleware.WriteError(w, 500, "ошибка записи файла")
			return
		}
		// Дедуплицированный blob уже принадлежит другим вложениям: его нельзя
		// удалять при откате этой загрузки.
		staged = append(staged, stagedFile{name: header.Filename, path: path, size: header.Size,
			contentSHA256: blob.SHA256, shared: blob.Deduplicated})
		if err := filestore.Validate(header.Filename, path, h.UploadDir); err != nil {
			middleware.WriteError(w, 400, err.Error())
			return
		}
		// STORE-05: угроза и недоступный сканер — разные исходы. Заражённый
		// файл не сохраняется никогда; непроверенный принимается только если
		// это разрешено политикой, и тогда лежит в карантине.
		scan, scanErr := filestore.Inspect(r.Context(), h.ScannerAddress, path, h.UploadDir)
		if scanErr != nil {
			middleware.WriteError(w, 500, "ошибка антивирусной проверки")
			return
		}
		switch scan.Verdict {
		case filestore.VerdictInfected:
			middleware.WriteError(w, 422, "файл не прошёл антивирусную проверку")
			return
		case filestore.VerdictUnavailable:
			if h.ScannerPolicy != "quarantine" {
				middleware.WriteError(w, 422, "антивирус недоступен, файл не принят")
				return
			}
			staged[len(staged)-1].scanStatus = "quarantined"
		default:
			staged[len(staged)-1].scanStatus = "clean"
		}
		staged[len(staged)-1].scanSignature = scan.Signature
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	// Lock per uploader: unrelated users do not block one another.
	var active bool
	var role, entity string
	var assigned, assignedCompany sql.NullString
	if tx.QueryRowContext(r.Context(), `SELECT is_active,role,COALESCE(entity_type,''),partner_id,it_company_id FROM users WHERE id=$1 FOR NO KEY UPDATE`, u.ID).Scan(&active, &role, &entity, &assigned, &assignedCompany) != nil || !active {
		middleware.WriteError(w, 403, "учётная запись недоступна")
		return
	}
	current := middleware.AuthUser{ID: u.ID, Role: models.Role(role), EntityType: models.EntityType(entity)}
	if assigned.Valid {
		current.PartnerID = &assigned.String
	}
	if assignedCompany.Valid {
		current.ITCompanyID = &assignedCompany.String
	}
	if !canUploadDocument(current, category, documentType) {
		middleware.WriteError(w, 403, "полномочия на загрузку документа изменились")
		return
	}
	var partner sql.NullString
	if tx.QueryRowContext(r.Context(), `SELECT partner_id FROM entries WHERE id::text=$1 FOR SHARE`, entryID).Scan(&partner) != nil {
		middleware.WriteError(w, 404, "запись не найдена")
		return
	}
	if !requirePartnerTenant(w, r, h.DB, current, partner.String) {
		return
	}
	quota := h.QuotaBytes
	if quota <= 0 {
		quota = 1 << 30
	}
	var used int64
	if tx.QueryRowContext(r.Context(), `SELECT COALESCE(sum(size_bytes),0) FROM attachments WHERE uploaded_by=$1`, u.ID).Scan(&used) != nil {
		middleware.WriteError(w, 500, "ошибка проверки квоты")
		return
	}
	if used > quota-total {
		middleware.WriteError(w, 413, "исчерпана квота хранения пользователя")
		return
	}
	days := 365
	var raw string
	if err := tx.QueryRowContext(r.Context(), `SELECT value FROM settings WHERE key='attachment_retention_days'`).Scan(&raw); err != nil {
		middleware.WriteError(w, 500, "ошибка чтения срока хранения")
		return
	}
	if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 3650 {
		days = v
	} else {
		middleware.WriteError(w, 500, "некорректный срок хранения")
		return
	}
	expires := time.Now().AddDate(0, 0, days)
	out := make([]map[string]interface{}, 0, len(staged))
	for _, f := range staged {
		var id string
		if tx.QueryRowContext(r.Context(), `INSERT INTO attachments(entry_id,owner_type,owner_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at,document_type,content_sha256,scan_status,scan_signature,scanned_at) VALUES($1,'entry',$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),now()) RETURNING id`, entryID, f.name, f.path, "application/octet-stream", f.size, u.ID, expires, documentType, f.contentSHA256, f.scanStatus, f.scanSignature).Scan(&id) != nil {
			middleware.WriteError(w, 500, "ошибка метаданных")
			return
		}
		item := map[string]interface{}{"id": id, "file_name": f.name, "size_bytes": f.size, "content_sha256": f.contentSHA256, "retention_expires_at": expires, "document_type": documentType, "review_status": "pending", "scan_status": f.scanStatus}
		if logAudit(r.Context(), tx, "attachment", id, "upload", u.ID, fmt.Sprintf("файл %s", f.name), nil, item) != nil {
			middleware.WriteError(w, 500, "ошибка аудита")
			return
		}
		out = append(out, item)
	}
	commitAttempted = true
	if err := tx.Commit(); err != nil {
		// A lost connection can make commit outcome uncertain. Retain files:
		// deleting them here could corrupt an already committed batch.
		slog.Error("upload commit outcome uncertain", "entry_id", entryID)
		middleware.WriteError(w, 503, "результат сохранения неизвестен; обновите список вложений перед повторной загрузкой")
		return
	}
	middleware.WriteJSON(w, 201, map[string]interface{}{"files": out, "id": out[0]["id"], "retention_expires_at": expires})
}

func (h *AttachmentHandlers) ListForEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,file_name,size_bytes,uploaded_at,retention_expires_at,document_type,review_status,
		COALESCE(review_comment,''),COALESCE(content_sha256,''),scan_status,owner_type,owner_id::text,
		COALESCE(document_date::text,''),COALESCE(valid_from::text,''),COALESCE(valid_until::text,''),
		COALESCE(signer_name,''),COALESCE(signer_certificate_id,''),COALESCE(signer_key_id,''),
		COALESCE(signature_algorithm,''),legal_dispute,COALESCE(dispute_reason,''),metadata_version
		FROM attachments WHERE entry_id=$1 AND retention_expires_at>now() ORDER BY uploaded_at DESC,id`+page, entryID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, name, documentType, reviewStatus, reviewComment, contentSHA256, scanStatus string
		var ownerType, ownerID, documentDate, validFrom, validUntil, signerName, signerCertificateID, signerKeyID, signatureAlgorithm, disputeReason string
		var legalDispute bool
		var metadataVersion int64
		var size int64
		var uploaded, expires time.Time
		if rows.Scan(&id, &name, &size, &uploaded, &expires, &documentType, &reviewStatus, &reviewComment, &contentSHA256, &scanStatus,
			&ownerType, &ownerID, &documentDate, &validFrom, &validUntil, &signerName, &signerCertificateID, &signerKeyID,
			&signatureAlgorithm, &legalDispute, &disputeReason, &metadataVersion) != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		list = append(list, map[string]interface{}{"id": id, "entry_id": entryID, "owner_type": ownerType, "owner_id": ownerID,
			"file_name": name, "size_bytes": size, "content_sha256": contentSHA256, "uploaded_at": uploaded,
			"retention_expires_at": expires, "document_type": documentType, "document_date": documentDate,
			"valid_from": validFrom, "valid_until": validUntil, "signer_name": signerName,
			"signer_certificate_id": signerCertificateID, "signer_key_id": signerKeyID,
			"signature_algorithm": signatureAlgorithm, "review_status": reviewStatus, "review_comment": reviewComment,
			"legal_dispute": legalDispute, "dispute_reason": disputeReason, "metadata_version": metadataVersion, "scan_status": scanStatus})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения")
		return
	}
	writePage(w, r, list)
}

type attachmentReviewRequest struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}

// Review records legal verification separately from the uploaded bytes. A
// reviewer cannot replace the file or alter financial data.
func (h *AttachmentHandlers) Review(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	if u.Role != models.RoleSuperAdmin && u.Role != models.RoleHoldingAdmin && u.Role != models.RoleOrgAdmin && u.Role != models.RoleLegalSpecialist {
		middleware.WriteError(w, 403, "проверка документов доступна только уполномоченному проверяющему")
		return
	}
	var entryID string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT entry_id::text FROM attachments WHERE id::text=$1 AND retention_expires_at>now()`, attachmentID).Scan(&entryID); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "документ не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения документа")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var req attachmentReviewRequest
	if decodeJSON(r, &req) != nil || (req.Status != "approved" && req.Status != "rejected") {
		middleware.WriteError(w, 400, "статус должен быть approved или rejected")
		return
	}
	req.Comment = strings.TrimSpace(req.Comment)
	if req.Status == "rejected" && req.Comment == "" {
		middleware.WriteError(w, 400, "при отклонении укажите причину")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), `UPDATE attachments SET review_status=$1,reviewed_by=$2,reviewed_at=now(),review_comment=NULLIF($3,'') WHERE id::text=$4`, req.Status, u.ID, req.Comment, attachmentID); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения проверки")
		return
	}
	if logAudit(r.Context(), tx, "attachment", attachmentID, "review", u.ID, req.Comment, nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения проверки")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"status": req.Status})
}

type attachmentMetadataInput struct {
	DocumentType        string `json:"document_type"`
	DocumentDate        string `json:"document_date"`
	ValidFrom           string `json:"valid_from"`
	ValidUntil          string `json:"valid_until"`
	SignerName          string `json:"signer_name"`
	SignerCertificateID string `json:"signer_certificate_id"`
	SignerKeyID         string `json:"signer_key_id"`
	SignatureAlgorithm  string `json:"signature_algorithm"`
	Version             int64  `json:"version"`
}

func validOptionalDate(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

// UpdateMetadata changes legal descriptors, never the immutable blob bytes.
func (h *AttachmentHandlers) UpdateMetadata(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var entryID, category string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT attachment.entry_id::text,entry.category_code
		FROM attachments attachment JOIN entries entry ON entry.id=attachment.entry_id
		WHERE attachment.id::text=$1 AND attachment.retention_expires_at>now()`, attachmentID).Scan(&entryID, &category); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "документ не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения документа")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var input attachmentMetadataInput
	if decodeJSON(r, &input) != nil {
		middleware.WriteError(w, 400, "некорректные метаданные документа")
		return
	}
	input.DocumentType = strings.TrimSpace(input.DocumentType)
	input.DocumentDate = strings.TrimSpace(input.DocumentDate)
	input.ValidFrom = strings.TrimSpace(input.ValidFrom)
	input.ValidUntil = strings.TrimSpace(input.ValidUntil)
	if !compliance.ValidDocumentType(input.DocumentType) || !canUploadDocument(u, category, input.DocumentType) {
		middleware.WriteError(w, 403, "роль не может назначить этот тип документа")
		return
	}
	if input.Version < 1 || !validOptionalDate(input.DocumentDate) || !validOptionalDate(input.ValidFrom) || !validOptionalDate(input.ValidUntil) ||
		(input.ValidFrom != "" && input.ValidUntil != "" && input.ValidUntil < input.ValidFrom) {
		middleware.WriteError(w, 400, "проверьте версию и даты документа")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE attachments SET document_type=$1,document_date=NULLIF($2,'')::date,
		valid_from=NULLIF($3,'')::date,valid_until=NULLIF($4,'')::date,signer_name=NULLIF($5,''),
		signer_certificate_id=NULLIF($6,''),signer_key_id=NULLIF($7,''),signature_algorithm=NULLIF($8,''),
		metadata_version=metadata_version+1 WHERE id::text=$9 AND metadata_version=$10`, input.DocumentType,
		input.DocumentDate, input.ValidFrom, input.ValidUntil, strings.TrimSpace(input.SignerName),
		strings.TrimSpace(input.SignerCertificateID), strings.TrimSpace(input.SignerKeyID), strings.TrimSpace(input.SignatureAlgorithm),
		attachmentID, input.Version)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения метаданных")
		return
	}
	changed, _ := result.RowsAffected()
	if changed == 0 {
		middleware.WriteError(w, 409, "метаданные документа уже изменены; обновите карточку")
		return
	}
	if err := logAudit(r.Context(), tx, "attachment", attachmentID, "metadata_update", u.ID, "", nil, input); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка аудита метаданных")
		return
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"id": attachmentID, "version": input.Version + 1})
}

type attachmentDisputeInput struct {
	Active bool   `json:"active"`
	Reason string `json:"reason"`
}

func (h *AttachmentHandlers) SetDispute(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	if u.Role != models.RoleSuperAdmin && u.Role != models.RoleHoldingAdmin && u.Role != models.RoleOrgAdmin && u.Role != models.RoleLegalSpecialist {
		middleware.WriteError(w, 403, "юридическое сомнение доступно уполномоченному проверяющему")
		return
	}
	var entryID string
	if err := h.DB.QueryRowContext(r.Context(), `SELECT entry_id::text FROM attachments WHERE id::text=$1`, attachmentID).Scan(&entryID); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "документ не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения документа")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var input attachmentDisputeInput
	if decodeJSON(r, &input) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Active && input.Reason == "" {
		middleware.WriteError(w, 400, "укажите причину юридического сомнения")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(r.Context(), `UPDATE attachments SET legal_dispute=$1,
		dispute_reason=CASE WHEN $1 THEN $2 ELSE NULL END,disputed_by=CASE WHEN $1 THEN $3::uuid ELSE NULL END,
		disputed_at=CASE WHEN $1 THEN now() ELSE NULL END,metadata_version=metadata_version+1 WHERE id::text=$4`,
		input.Active, input.Reason, u.ID, attachmentID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения юридического сомнения")
		return
	}
	if err := logAudit(r.Context(), tx, "attachment", attachmentID, "legal_dispute", u.ID, input.Reason, nil, input); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка аудита юридического сомнения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"id": attachmentID, "legal_dispute": input.Active})
}

func (h *AttachmentHandlers) Download(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var name, path, entry, scanStatus string
	var expectedSHA256 sql.NullString
	var expires time.Time
	err := h.DB.QueryRowContext(r.Context(), "SELECT file_name,storage_path,entry_id,retention_expires_at,content_sha256,scan_status FROM attachments WHERE id::text=$1", attachmentID).Scan(&name, &path, &entry, &expires, &expectedSHA256, &scanStatus)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "файл не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса")
		return
	}
	if !requireEntry(w, r, h.DB, u, entry) {
		return
	}
	if !expires.After(time.Now()) {
		middleware.WriteError(w, 410, "срок хранения файла истёк")
		return
	}
	// STORE-05: файл, принятый без антивирусной проверки, не выдаётся, пока
	// проверка не пройдена: иначе карантин ничего не значит.
	if scanStatus == "quarantined" {
		middleware.WriteError(w, http.StatusConflict, "файл в карантине: антивирусная проверка не пройдена")
		return
	}
	f, err := filestore.Open(h.UploadDir, path)
	if err != nil {
		middleware.WriteError(w, 410, "файл недоступен")
		return
	}
	defer f.Close()
	if expectedSHA256.Valid {
		digest := sha256.New()
		if _, err := io.Copy(digest, f); err != nil || hex.EncodeToString(digest.Sum(nil)) != expectedSHA256.String {
			middleware.WriteError(w, 410, "целостность файла нарушена")
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			middleware.WriteError(w, 500, "не удалось подготовить файл")
			return
		}
		w.Header().Set("X-Content-SHA256", expectedSHA256.String)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if _, err := io.Copy(w, f); err != nil {
		slog.Warn("download interrupted", "attachment_id", attachmentID)
	}
}
