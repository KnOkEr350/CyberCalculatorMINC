package handlers

import (
	"crypto/rand"
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
	QuotaBytes     int64
}

const maxAttachmentSize int64 = 20 << 20
const maxAttachmentCount = 20

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

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
	}
	staged := make([]stagedFile, 0, len(headers))
	// Files are unreferenced until the short metadata transaction commits.
	// Do not hold a database connection or quota lock during scanning.
	commitAttempted := false
	defer func() {
		if !commitAttempted {
			for _, f := range staged {
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
		filename, err := randomHex(16)
		if err != nil {
			src.Close()
			middleware.WriteError(w, 500, "ошибка создания файла")
			return
		}
		dst, path, err := filestore.Create(h.UploadDir, entryID, filename)
		if err != nil {
			src.Close()
			middleware.WriteError(w, 500, "не удалось сохранить файл")
			return
		}
		digest := sha256.New()
		staged = append(staged, stagedFile{name: header.Filename, path: path, size: header.Size})
		size, copyErr := io.Copy(io.MultiWriter(dst, digest), io.LimitReader(src, maxAttachmentSize+1))
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil || closeErr != nil || size != header.Size {
			middleware.WriteError(w, 500, "ошибка записи файла")
			return
		}
		staged[len(staged)-1].contentSHA256 = hex.EncodeToString(digest.Sum(nil))
		if err := filestore.Validate(header.Filename, path, h.UploadDir); err != nil {
			middleware.WriteError(w, 400, err.Error())
			return
		}
		if err := filestore.Scan(r.Context(), h.ScannerAddress, path, h.UploadDir); err != nil {
			middleware.WriteError(w, 422, "файл не прошёл проверку или антивирус недоступен")
			return
		}
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
		if tx.QueryRowContext(r.Context(), `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at,document_type,content_sha256) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, entryID, f.name, f.path, "application/octet-stream", f.size, u.ID, expires, documentType, f.contentSHA256).Scan(&id) != nil {
			middleware.WriteError(w, 500, "ошибка метаданных")
			return
		}
		item := map[string]interface{}{"id": id, "file_name": f.name, "size_bytes": f.size, "content_sha256": f.contentSHA256, "retention_expires_at": expires, "document_type": documentType, "review_status": "pending"}
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
	rows, err := h.DB.QueryContext(r.Context(), "SELECT id,file_name,size_bytes,uploaded_at,retention_expires_at,document_type,review_status,COALESCE(review_comment,''),COALESCE(content_sha256,'') FROM attachments WHERE entry_id=$1 AND retention_expires_at>now() ORDER BY uploaded_at DESC,id"+page, entryID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, name, documentType, reviewStatus, reviewComment, contentSHA256 string
		var size int64
		var uploaded, expires time.Time
		if rows.Scan(&id, &name, &size, &uploaded, &expires, &documentType, &reviewStatus, &reviewComment, &contentSHA256) != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		list = append(list, map[string]interface{}{"id": id, "entry_id": entryID, "file_name": name, "size_bytes": size, "content_sha256": contentSHA256, "uploaded_at": uploaded, "retention_expires_at": expires, "document_type": documentType, "review_status": reviewStatus, "review_comment": reviewComment})
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

func (h *AttachmentHandlers) Download(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var name, path, entry string
	var expectedSHA256 sql.NullString
	var expires time.Time
	err := h.DB.QueryRowContext(r.Context(), "SELECT file_name,storage_path,entry_id,retention_expires_at,content_sha256 FROM attachments WHERE id::text=$1", attachmentID).Scan(&name, &path, &entry, &expires, &expectedSHA256)
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
