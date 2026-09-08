package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"cybercalc/internal/middleware"
)

type AttachmentHandlers struct {
	DB        *sql.DB
	UploadDir string
}

const maxAttachmentSize int64 = 64 << 20

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Upload — «сохранять файл с подтверждением (документ)» при занесении факта.
// Срок хранения берётся из settings.attachment_retention_days (по умолчанию
// 365 дней/год, администратор может изменить).
func (h *AttachmentHandlers) Upload(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	var periodType string
	err := h.DB.QueryRow(`SELECT period_type FROM entries WHERE id = $1`, entryID).Scan(&periodType)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "запись не найдена")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	if periodType != "fact" {
		middleware.WriteError(w, http.StatusBadRequest, "подтверждающий документ прикладывается только к факту")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttachmentSize+(1<<20)) // запас для служебных данных multipart
	if err := r.ParseMultipartForm(8 << 20); err != nil {              // до 8 МБ в памяти, остальное — во временных файлах
		middleware.WriteError(w, http.StatusBadRequest, "не удалось разобрать форму (ожидается multipart/form-data, поле file)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "поле file обязательно")
		return
	}
	defer file.Close()

	retentionDays := 365
	var raw string
	if err := h.DB.QueryRow(`SELECT value FROM settings WHERE key = 'attachment_retention_days'`).Scan(&raw); err == nil {
		if v, convErr := strconv.Atoi(raw); convErr == nil {
			retentionDays = v
		}
	}

	entryDir := filepath.Join(h.UploadDir, entryID)
	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось создать каталог для файла")
		return
	}
	storedName := randomHex(16) + filepath.Ext(header.Filename)
	storagePath := filepath.Join(entryDir, storedName)

	dst, err := os.OpenFile(storagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось сохранить файл")
		return
	}
	defer dst.Close()
	size, err := io.Copy(dst, io.LimitReader(file, maxAttachmentSize+1))
	if err != nil {
		os.Remove(storagePath)
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи файла")
		return
	}
	if size > maxAttachmentSize {
		os.Remove(storagePath)
		middleware.WriteError(w, http.StatusRequestEntityTooLarge, "размер файла превышает 64 МБ")
		return
	}

	contentType := header.Header.Get("Content-Type")
	expiresAt := time.Now().AddDate(0, 0, retentionDays)

	var id string
	err = h.DB.QueryRow(
		`INSERT INTO attachments (entry_id, file_name, storage_path, content_type, size_bytes, uploaded_by, retention_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		entryID, header.Filename, storagePath, contentType, size, u.ID, expiresAt,
	).Scan(&id)
	if err != nil {
		os.Remove(storagePath)
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения метаданных файла")
		return
	}

	logAudit(h.DB, "attachment", id, "upload", u.ID, fmt.Sprintf("файл %s (%d байт)", header.Filename, size), nil,
		map[string]interface{}{"entry_id": entryID, "file_name": header.Filename})

	middleware.WriteJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "retention_expires_at": expiresAt})
}

func (h *AttachmentHandlers) ListForEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	rows, err := h.DB.Query(`SELECT id, entry_id, file_name, content_type, size_bytes, uploaded_by, uploaded_at, retention_expires_at
		FROM attachments WHERE entry_id = $1 ORDER BY uploaded_at DESC`, entryID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()
	type out struct {
		ID                 string    `json:"id"`
		EntryID            string    `json:"entry_id"`
		FileName           string    `json:"file_name"`
		ContentType        string    `json:"content_type"`
		SizeBytes          int64     `json:"size_bytes"`
		UploadedBy         string    `json:"uploaded_by"`
		UploadedAt         time.Time `json:"uploaded_at"`
		RetentionExpiresAt time.Time `json:"retention_expires_at"`
	}
	list := make([]out, 0)
	for rows.Next() {
		var o out
		if err := rows.Scan(&o.ID, &o.EntryID, &o.FileName, &o.ContentType, &o.SizeBytes, &o.UploadedBy, &o.UploadedAt, &o.RetentionExpiresAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		list = append(list, o)
	}
	middleware.WriteJSON(w, http.StatusOK, list)
}

func (h *AttachmentHandlers) Download(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var fileName, storagePath, contentType string
	err := h.DB.QueryRow(`SELECT file_name, storage_path, content_type FROM attachments WHERE id = $1`, attachmentID).
		Scan(&fileName, &storagePath, &contentType)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "файл не найден")
		return
	} else if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	f, err := os.Open(storagePath)
	if err != nil {
		middleware.WriteError(w, http.StatusGone, "файл удалён по истечении срока хранения")
		return
	}
	defer f.Close()
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	io.Copy(w, f)
}
