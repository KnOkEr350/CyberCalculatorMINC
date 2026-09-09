package handlers

import (
	"crypto/rand"
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
	"time"
)

type AttachmentHandlers struct {
	DB             *sql.DB
	UploadDir      string
	ScannerAddress string
	QuotaBytes     int64
}

const maxAttachmentSize int64 = 64 << 20
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
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		middleware.WriteError(w, 400, "ожидаются файлы multipart/form-data, не более 64 МБ суммарно")
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
		middleware.WriteError(w, 413, "суммарный размер превышает 64 МБ")
		return
	}
	type stagedFile struct {
		name, path string
		size       int64
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
		staged = append(staged, stagedFile{header.Filename, path, header.Size})
		size, copyErr := io.Copy(dst, io.LimitReader(src, maxAttachmentSize+1))
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil || closeErr != nil || size != header.Size {
			middleware.WriteError(w, 500, "ошибка записи файла")
			return
		}
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
	var assigned sql.NullString
	if tx.QueryRowContext(r.Context(), `SELECT is_active,role,COALESCE(entity_type,''),partner_id FROM users WHERE id=$1 FOR NO KEY UPDATE`, u.ID).Scan(&active, &role, &entity, &assigned) != nil || !active {
		middleware.WriteError(w, 403, "учётная запись недоступна")
		return
	}
	current := middleware.AuthUser{ID: u.ID, Role: models.Role(role), EntityType: models.EntityType(entity)}
	if assigned.Valid {
		current.PartnerID = &assigned.String
	}
	var partner sql.NullString
	if tx.QueryRowContext(r.Context(), `SELECT partner_id FROM entries WHERE id::text=$1 FOR SHARE`, entryID).Scan(&partner) != nil {
		middleware.WriteError(w, 404, "запись не найдена")
		return
	}
	if !requirePartner(w, current, partner.String) {
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
		if tx.QueryRowContext(r.Context(), `INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`, entryID, f.name, f.path, "application/octet-stream", f.size, u.ID, expires).Scan(&id) != nil {
			middleware.WriteError(w, 500, "ошибка метаданных")
			return
		}
		item := map[string]interface{}{"id": id, "file_name": f.name, "size_bytes": f.size, "retention_expires_at": expires}
		if logAudit(tx, "attachment", id, "upload", u.ID, fmt.Sprintf("файл %s", f.name), nil, item) != nil {
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
	rows, err := h.DB.QueryContext(r.Context(), "SELECT id,file_name,size_bytes,uploaded_at,retention_expires_at FROM attachments WHERE entry_id=$1 AND retention_expires_at>now() ORDER BY uploaded_at DESC,id"+page, entryID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, name string
		var size int64
		var uploaded, expires time.Time
		if rows.Scan(&id, &name, &size, &uploaded, &expires) != nil {
			middleware.WriteError(w, 500, "ошибка чтения")
			return
		}
		list = append(list, map[string]interface{}{"id": id, "entry_id": entryID, "file_name": name, "size_bytes": size, "uploaded_at": uploaded, "retention_expires_at": expires})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения")
		return
	}
	writePage(w, r, list)
}

func (h *AttachmentHandlers) Download(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var name, path, entry string
	var expires time.Time
	err := h.DB.QueryRowContext(r.Context(), "SELECT file_name,storage_path,entry_id,retention_expires_at FROM attachments WHERE id::text=$1", attachmentID).Scan(&name, &path, &entry, &expires)
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
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	if _, err := io.Copy(w, f); err != nil {
		slog.Warn("download interrupted", "attachment_id", attachmentID)
	}
}
