package handlers

import (
	"crypto/rand"
	"cybercalc/internal/filestore"
	"cybercalc/internal/middleware"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
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
	if !requireEntry(w, h.DB, u, entryID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
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
		total += f.Size
		if f.Size <= 0 || len([]rune(f.Filename)) > 255 {
			middleware.WriteError(w, 400, "пустой файл или слишком длинное имя")
			return
		}
	}
	if total > maxAttachmentSize {
		middleware.WriteError(w, 413, "суммарный размер превышает 64 МБ")
		return
	}
	days := 365
	var raw string
	if h.DB.QueryRowContext(r.Context(), "SELECT value FROM settings WHERE key='attachment_retention_days'").Scan(&raw) == nil {
		if v, e := strconv.Atoi(raw); e == nil && v > 0 && v <= 3650 {
			days = v
		}
	}
	expires := time.Now().AddDate(0, 0, days)
	dir := filepath.Join(h.UploadDir, entryID)
	if os.MkdirAll(dir, 0750) != nil {
		middleware.WriteError(w, 500, "не удалось создать каталог")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	// One quota lock across replicas prevents concurrent uploads exceeding quota.
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(270006)`); err != nil {
		middleware.WriteError(w, 503, "загрузка временно недоступна")
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
	if used+total > quota {
		middleware.WriteError(w, 413, "исчерпана квота хранения пользователя")
		return
	}
	paths := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, p := range paths {
				os.Remove(p)
			}
		}
	}()
	out := []map[string]interface{}{}
	for _, header := range headers {
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
		path := filepath.Join(dir, filename)
		dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
		if err != nil {
			src.Close()
			middleware.WriteError(w, 500, "не удалось сохранить файл")
			return
		}
		paths = append(paths, path)
		size, copyErr := io.Copy(dst, io.LimitReader(src, maxAttachmentSize+1))
		closeErr := dst.Close()
		src.Close()
		if copyErr != nil || closeErr != nil || size != header.Size {
			middleware.WriteError(w, 500, "ошибка записи файла")
			return
		}
		if err := filestore.Validate(header.Filename, path); err != nil {
			middleware.WriteError(w, 400, err.Error())
			return
		}
		if err := filestore.Scan(r.Context(), h.ScannerAddress, path); err != nil {
			middleware.WriteError(w, 422, "файл не прошёл проверку или антивирус недоступен")
			return
		}
		var id string
		err = tx.QueryRowContext(r.Context(), "INSERT INTO attachments(entry_id,file_name,storage_path,content_type,size_bytes,uploaded_by,retention_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id", entryID, header.Filename, path, "application/octet-stream", size, u.ID, expires).Scan(&id)
		if err != nil {
			middleware.WriteError(w, 500, "ошибка метаданных")
			return
		}
		item := map[string]interface{}{"id": id, "file_name": header.Filename, "size_bytes": size, "retention_expires_at": expires}
		if logAudit(tx, "attachment", id, "upload", u.ID, fmt.Sprintf("файл %s", header.Filename), nil, item) != nil {
			middleware.WriteError(w, 500, "ошибка аудита")
			return
		}
		out = append(out, item)
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения пакета")
		return
	}
	committed = true
	middleware.WriteJSON(w, 201, map[string]interface{}{"files": out, "id": out[0]["id"], "retention_expires_at": expires})
}

func (h *AttachmentHandlers) ListForEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	if !requireEntry(w, h.DB, u, entryID) {
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
	if !requireEntry(w, h.DB, u, entry) {
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
	io.Copy(w, f)
}
