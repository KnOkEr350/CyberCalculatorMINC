package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type attachmentMetadataRequest struct {
	DocumentNumber        string `json:"document_number"`
	DocumentDate          string `json:"document_date"`
	SignerName            string `json:"signer_name"`
	CertificateSerial     string `json:"certificate_serial"`
	CertificateValidFrom  string `json:"certificate_valid_from"`
	CertificateValidUntil string `json:"certificate_valid_until"`
}

// UpdateMetadata задаёт реквизиты документа и сведения о подписи (DATA-12).
//
// Право на это — у стороны-владельца документа: тот, кто вправе загрузить
// документ такого типа, вправе описать его реквизиты, но профиль другой
// стороны — нет. Изменение реквизитов возвращает документ на проверку:
// одобрение относилось к прежним сведениям, и оставлять его в силе нельзя.
func (h *AttachmentHandlers) UpdateMetadata(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, attachmentID string) {
	var req attachmentMetadataRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	var entryID, documentType, category, ownerEntity string
	err := h.DB.QueryRowContext(r.Context(), `SELECT a.entry_id::text,a.document_type,e.category_code,a.owner_entity_type
		FROM attachments a JOIN entries e ON e.id=a.entry_id
		WHERE a.id::text=$1 AND a.retention_expires_at>now()`, attachmentID).Scan(&entryID, &documentType, &category, &ownerEntity)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "документ не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения документа")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	if string(u.EntityType) != ownerEntity || !canUploadDocument(u, category, documentType) {
		middleware.WriteError(w, http.StatusForbidden, "реквизиты документа описывает его владелец")
		return
	}

	normalized, err := compliance.NormalizeDocumentMetadata(compliance.DocumentMetadata{
		Number: req.DocumentNumber, Date: req.DocumentDate, SignerName: req.SignerName,
		CertificateSerial: req.CertificateSerial, CertificateValidFrom: req.CertificateValidFrom,
		CertificateValidUntil: req.CertificateValidUntil,
	}, regulatoryToday())
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	// Проверка сбрасывается только если сведения действительно изменились:
	// повторная отправка тех же реквизитов не должна лишать документ одобрения.
	var changed bool
	if err := tx.QueryRowContext(r.Context(), `SELECT
		document_number IS DISTINCT FROM NULLIF($2,'') OR document_date IS DISTINCT FROM NULLIF($3,'')::date
		OR signer_name IS DISTINCT FROM NULLIF($4,'') OR certificate_serial IS DISTINCT FROM NULLIF($5,'')
		OR certificate_valid_from IS DISTINCT FROM NULLIF($6,'')::date OR certificate_valid_until IS DISTINCT FROM NULLIF($7,'')::date
		FROM attachments WHERE id::text=$1 FOR UPDATE`, attachmentID, normalized.Number, normalized.Date, normalized.SignerName,
		normalized.CertificateSerial, normalized.CertificateValidFrom, normalized.CertificateValidUntil).Scan(&changed); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения документа")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE attachments SET document_number=NULLIF($2,''),document_date=NULLIF($3,'')::date,
		signer_name=NULLIF($4,''),certificate_serial=NULLIF($5,''),certificate_valid_from=NULLIF($6,'')::date,
		certificate_valid_until=NULLIF($7,'')::date,
		review_status=CASE WHEN $8 THEN 'pending' ELSE review_status END,
		reviewed_by=CASE WHEN $8 THEN NULL ELSE reviewed_by END,
		reviewed_at=CASE WHEN $8 THEN NULL ELSE reviewed_at END,
		review_comment=CASE WHEN $8 THEN NULL ELSE review_comment END,
		signature_verified_at=CASE WHEN $8 THEN NULL ELSE signature_verified_at END,
		signature_verified_by=CASE WHEN $8 THEN NULL ELSE signature_verified_by END
		WHERE id::text=$1`, attachmentID, normalized.Number, normalized.Date, normalized.SignerName,
		normalized.CertificateSerial, normalized.CertificateValidFrom, normalized.CertificateValidUntil, changed); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения реквизитов")
		return
	}
	if logAudit(r.Context(), tx, "attachment", attachmentID, "metadata", u.ID, "", nil, normalized) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения реквизитов")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "review_reset": changed})
}

// regulatoryToday — московская дата «сегодня» для проверки дат документов.
func regulatoryToday() time.Time {
	year, month, day := time.Now().In(businessLocation).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// --- Юридическое сомнение (INT-08, ADR-17) ---------------------------------

type legalDisputeView struct {
	ID           int64  `json:"id"`
	EntryID      string `json:"entry_id"`
	AttachmentID string `json:"attachment_id,omitempty"`
	Reason       string `json:"reason"`
	RaisedBy     string `json:"raised_by"`
	RaisedAt     string `json:"raised_at"`
	LiftedBy     string `json:"lifted_by,omitempty"`
	LiftedAt     string `json:"lifted_at,omitempty"`
	LiftedReason string `json:"lifted_reason,omitempty"`
	Active       bool   `json:"active"`
}

type legalDisputeRequest struct {
	Action       string `json:"action"` // raise | lift
	Reason       string `json:"reason"`
	AttachmentID string `json:"attachment_id,omitempty"`
}

// canDisputeLegally — кто вправе поставить запись под сомнение и снять его:
// те же роли, что проверяют документы. Юрист при этом не получает права менять
// сами данные записи.
func canDisputeLegally(u middleware.AuthUser) bool {
	if u.EntityType != models.EntityOrganization {
		return false
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleLegalSpecialist:
		return true
	}
	return false
}

// LegalDisputes отдаёт историю сомнений по записи.
func (h *AttachmentHandlers) LegalDisputes(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,entry_id::text,COALESCE(attachment_id::text,''),reason,raised_by::text,
			raised_at::text,COALESCE(lifted_by::text,''),COALESCE(lifted_at::text,''),COALESCE(lifted_reason,''),lifted_at IS NULL
		FROM legal_disputes WHERE entry_id::text=$1 ORDER BY id`, entryID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения сомнений")
		return
	}
	defer rows.Close()
	out := make([]legalDisputeView, 0)
	for rows.Next() {
		var item legalDisputeView
		if err := rows.Scan(&item.ID, &item.EntryID, &item.AttachmentID, &item.Reason, &item.RaisedBy, &item.RaisedAt,
			&item.LiftedBy, &item.LiftedAt, &item.LiftedReason, &item.Active); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения сомнений")
			return
		}
		out = append(out, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения сомнений")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, out)
}

// ChangeLegalDispute ставит запись под сомнение или снимает его. Причина
// обязательна в обоих случаях: и постановка, и снятие меняют то, войдёт ли
// запись в зачётную сумму, и оба решения должны быть объяснимы.
func (h *AttachmentHandlers) ChangeLegalDispute(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, entryID string) {
	if !canDisputeLegally(u) {
		middleware.WriteError(w, http.StatusForbidden, "сомнение ставит и снимает юридическое управление")
		return
	}
	if !requireEntry(w, r, h.DB, u, entryID) {
		return
	}
	var req legalDisputeRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len([]rune(req.Reason)) > 2000 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите причину (до 2000 символов)")
		return
	}
	if req.Action != "raise" && req.Action != "lift" {
		middleware.WriteError(w, http.StatusBadRequest, "действие должно быть raise или lift")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()

	var id int64
	if req.Action == "raise" {
		if req.AttachmentID != "" {
			var belongs bool
			if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM attachments WHERE id::text=$1 AND entry_id::text=$2)`,
				req.AttachmentID, entryID).Scan(&belongs); err != nil || !belongs {
				middleware.WriteError(w, http.StatusBadRequest, "документ не относится к этой записи")
				return
			}
		}
		err := tx.QueryRowContext(r.Context(), `INSERT INTO legal_disputes(entry_id,attachment_id,reason,raised_by)
			VALUES($1::uuid,NULLIF($2,'')::uuid,$3,$4::uuid)
			ON CONFLICT (entry_id) WHERE lifted_at IS NULL DO NOTHING RETURNING id`,
			entryID, req.AttachmentID, req.Reason, u.ID).Scan(&id)
		if err == sql.ErrNoRows {
			middleware.WriteError(w, http.StatusConflict, "запись уже находится под сомнением")
			return
		}
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения сомнения")
			return
		}
	} else {
		err := tx.QueryRowContext(r.Context(), `UPDATE legal_disputes SET lifted_by=$2::uuid,lifted_at=now(),lifted_reason=$3
			WHERE entry_id::text=$1 AND lifted_at IS NULL RETURNING id`, entryID, u.ID, req.Reason).Scan(&id)
		if err == sql.ErrNoRows {
			middleware.WriteError(w, http.StatusConflict, "у записи нет действующего сомнения")
			return
		}
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка снятия сомнения")
			return
		}
	}
	if logAudit(r.Context(), tx, "legal_dispute", entryID, "legal_dispute_"+req.Action, u.ID, req.Reason, nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{"id": id, "action": req.Action})
}
