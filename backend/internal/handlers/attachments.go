package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/middleware"
)

type AttachmentHandlers struct {
	DB        *sql.DB
	UploadDir string
}

const maxAttachmentSize int64 = 64 << 20

var errAttachmentTooLarge = errors.New("размер файла превышает 64 МБ")

type attachmentStore interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

type storedAttachment struct {
	ID        string
	Path      string
	ExpiresAt time.Time
	FileName  string
	Size      int64
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Upload — «сохранять файл с подтверждением (документ)» при занесении факта.
// Срок хранения берётся из settings.attachment_retention_days (по умолчанию
// 365 дней/год, администратор может изменить).
func (h *AttachmentHandlers) Upload(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	conditions, args := appendEntryScope([]string{"id = $1"}, []interface{}{entryID}, u, "")
	var periodType string
	err := h.DB.QueryRow(`SELECT period_type FROM entries WHERE `+strings.Join(conditions, " AND "), args...).Scan(&periodType)
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

	tx, err := h.DB.Begin()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка начала транзакции")
		return
	}
	defer tx.Rollback()

	stored, err := storeAttachment(tx, h.UploadDir, entryID, u.ID, file, header)
	if err != nil {
		if errors.Is(err, errAttachmentTooLarge) {
			middleware.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
		} else {
			middleware.WriteError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(stored.Path)
		}
	}()

	if err := logAudit(tx, "attachment", stored.ID, "upload", u.ID, fmt.Sprintf("файл %s (%d байт)", stored.FileName, stored.Size), nil,
		map[string]interface{}{"entry_id": entryID, "file_name": stored.FileName}); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка записи журнала аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка завершения транзакции")
		return
	}
	committed = true
	middleware.WriteJSON(w, http.StatusCreated, map[string]interface{}{"id": stored.ID, "retention_expires_at": stored.ExpiresAt})
}

func storeAttachment(db attachmentStore, uploadDir, entryID, userID string, file multipart.File, header *multipart.FileHeader) (storedAttachment, error) {
	var result storedAttachment
	retentionDays := 365
	var raw string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'attachment_retention_days'`).Scan(&raw); err == nil {
		if v, convErr := strconv.Atoi(raw); convErr == nil {
			retentionDays = v
		}
	}

	entryDir := filepath.Join(uploadDir, entryID)
	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		return result, fmt.Errorf("не удалось создать каталог для файла")
	}
	storedName := randomHex(16) + filepath.Ext(header.Filename)
	storagePath := filepath.Join(entryDir, storedName)

	dst, err := os.OpenFile(storagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return result, fmt.Errorf("не удалось сохранить файл")
	}
	size, err := io.Copy(dst, io.LimitReader(file, maxAttachmentSize+1))
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(storagePath)
		return result, fmt.Errorf("ошибка записи файла")
	}
	if closeErr != nil {
		_ = os.Remove(storagePath)
		return result, fmt.Errorf("ошибка завершения записи файла")
	}
	if size > maxAttachmentSize {
		_ = os.Remove(storagePath)
		return result, errAttachmentTooLarge
	}

	fileName := filepath.Base(strings.TrimSpace(header.Filename))
	if fileName == "" || fileName == "." {
		_ = os.Remove(storagePath)
		return result, fmt.Errorf("имя файла не задано")
	}
	contentType := header.Header.Get("Content-Type")
	expiresAt := time.Now().AddDate(0, 0, retentionDays)

	var id string
	err = db.QueryRow(
		`INSERT INTO attachments (entry_id, file_name, storage_path, content_type, size_bytes, uploaded_by, retention_expires_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		entryID, fileName, storagePath, contentType, size, userID, expiresAt,
	).Scan(&id)
	if err != nil {
		_ = os.Remove(storagePath)
		return result, fmt.Errorf("ошибка сохранения метаданных файла")
	}
	return storedAttachment{ID: id, Path: storagePath, ExpiresAt: expiresAt, FileName: fileName, Size: size}, nil
}

func (h *AttachmentHandlers) ListForEntry(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	conditions, args := appendEntryScope([]string{"a.entry_id = $1"}, []interface{}{entryID}, u, "e")
	rows, err := h.DB.Query(`SELECT a.id, a.entry_id, a.file_name, a.content_type, a.size_bytes, a.uploaded_by, a.uploaded_at, a.retention_expires_at
		FROM attachments a JOIN entries e ON e.id = a.entry_id WHERE `+strings.Join(conditions, " AND ")+` ORDER BY a.uploaded_at DESC`, args...)
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
	conditions, args := appendEntryScope([]string{"a.id = $1"}, []interface{}{attachmentID}, u, "e")
	var fileName, storagePath, contentType string
	err := h.DB.QueryRow(`SELECT a.file_name, a.storage_path, a.content_type FROM attachments a JOIN entries e ON e.id = a.entry_id WHERE `+
		strings.Join(conditions, " AND "), args...).
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
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	io.Copy(w, f)
}
