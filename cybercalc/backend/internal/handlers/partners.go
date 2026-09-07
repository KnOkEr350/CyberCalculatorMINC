package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type PartnerHandlers struct {
	DB *sql.DB
}

func (h *PartnerHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	rows, err := h.DB.Query(`SELECT id, name, partner_kind, agreement_number, other_agreement, created_at
		FROM partners ORDER BY name`)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	var partners []models.Partner
	for rows.Next() {
		var p models.Partner
		var agreementNumber, otherAgreement sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.PartnerKind, &agreementNumber, &otherAgreement, &p.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		p.AgreementNumber = agreementNumber.String
		p.OtherAgreement = otherAgreement.String
		partners = append(partners, p)
	}
	middleware.WriteJSON(w, http.StatusOK, partners)
}

type createPartnerRequest struct {
	Name            string `json:"name"`
	PartnerKind     string `json:"partner_kind"`
	AgreementDate   string `json:"agreement_date,omitempty"`
	AgreementNumber string `json:"agreement_number,omitempty"`
	OtherAgreement  string `json:"other_agreement,omitempty"`
}

func (h *PartnerHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	var req createPartnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if req.Name == "" || (req.PartnerKind != "vuz" && req.PartnerKind != "kolledj" && req.PartnerKind != "school") {
		middleware.WriteError(w, http.StatusBadRequest, "укажите name и partner_kind (vuz|kolledj|school)")
		return
	}

	var id string
	var agreementDate interface{}
	if req.AgreementDate != "" {
		agreementDate = req.AgreementDate
	}
	err := h.DB.QueryRow(
		`INSERT INTO partners (name, partner_kind, agreement_date, agreement_number, other_agreement)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		req.Name, req.PartnerKind, agreementDate, req.AgreementNumber, req.OtherAgreement,
	).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка сохранения")
		return
	}
	logAudit(h.DB, "partner", id, "create", u.ID, "", nil, req)
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}
