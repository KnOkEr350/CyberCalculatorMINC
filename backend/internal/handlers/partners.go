package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type PartnerHandlers struct {
	DB *sql.DB
}

func (h *PartnerHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	rows, err := h.DB.Query(`SELECT id, name, partner_kind, agreement_number, other_agreement, created_at, agreement_date
		FROM partners WHERE ($1='' OR id::text=$1) ORDER BY name`, partnerScope(u, ""))
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	partners := make([]models.Partner, 0)
	for rows.Next() {
		var p models.Partner
		var agreementNumber, otherAgreement sql.NullString
		var agreementDate sql.NullTime
		if err := rows.Scan(&p.ID, &p.Name, &p.PartnerKind, &agreementNumber, &otherAgreement, &p.CreatedAt, &agreementDate); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		p.AgreementNumber = agreementNumber.String
		p.OtherAgreement = otherAgreement.String
		if agreementDate.Valid {
			date := agreementDate.Time.Format("2006-01-02")
			p.AgreementDate = &date
		}
		partners = append(partners, p)
	}
	middleware.WriteJSON(w, http.StatusOK, partners)
}

type createPartnerRequest struct {
	DirectoryID     string `json:"directory_id,omitempty"`
	Name            string `json:"name"`
	PartnerKind     string `json:"partner_kind"`
	AgreementDate   string `json:"agreement_date,omitempty"`
	AgreementNumber string `json:"agreement_number,omitempty"`
	OtherAgreement  string `json:"other_agreement,omitempty"`
}

func (h *PartnerHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "партнёров добавляет сотрудник Киберпротекта")
		return
	}
	var req createPartnerRequest
	if err := decodeJSON(r, &req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.DirectoryID != "" {
		if err := h.DB.QueryRow(`SELECT name,partner_kind FROM education_directory WHERE id::text=$1`, req.DirectoryID).Scan(&req.Name, &req.PartnerKind); err != nil {
			middleware.WriteError(w, 400, "организация не найдена в справочнике")
			return
		}
	}
	req.AgreementNumber = strings.TrimSpace(req.AgreementNumber)
	req.OtherAgreement = strings.TrimSpace(req.OtherAgreement)
	if req.Name == "" || len([]rune(req.Name)) > 1000 || (req.PartnerKind != "vuz" && req.PartnerKind != "kolledj" && req.PartnerKind != "school") {
		middleware.WriteError(w, http.StatusBadRequest, "укажите name и partner_kind (vuz|kolledj|school)")
		return
	}
	if len([]rune(req.AgreementNumber)) > 100 || len([]rune(req.OtherAgreement)) > 1000 {
		middleware.WriteError(w, http.StatusBadRequest, "реквизиты соглашения слишком длинные")
		return
	}
	if req.AgreementDate != "" {
		if _, err := time.Parse("2006-01-02", req.AgreementDate); err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "дата соглашения должна быть в формате ГГГГ-ММ-ДД")
			return
		}
	}

	var id string
	var agreementDate interface{}
	if req.AgreementDate != "" {
		agreementDate = req.AgreementDate
	}
	tx, err := h.DB.Begin()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	err = tx.QueryRow(
		`INSERT INTO partners (name, partner_kind, agreement_date, agreement_number, other_agreement, directory_id)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6,'')::uuid) RETURNING id`,
		req.Name, req.PartnerKind, agreementDate, req.AgreementNumber, req.OtherAgreement, req.DirectoryID,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось сохранить партнёра: возможно, организация уже добавлена")
		return
	}
	if err := logAudit(tx, "partner", id, "create", u.ID, "", nil, req); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}
