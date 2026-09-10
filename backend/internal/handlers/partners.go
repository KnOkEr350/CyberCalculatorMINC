package handlers

import (
	"database/sql"
	"net/http"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type PartnerHandlers struct {
	DB *sql.DB
}

func (h *PartnerHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT p.id,p.name,p.partner_kind,COALESCE(p.directory_id::text,''),
		COALESCE(latest.number,''),latest.signed_on,p.other_agreement,p.created_at,
		(SELECT count(*) FROM agreement_partners ap WHERE ap.partner_id=p.id),
		(SELECT count(*) FROM agreement_partners ap JOIN agreements a ON a.id=ap.agreement_id
		 WHERE ap.partner_id=p.id AND a.status='active' AND CURRENT_DATE BETWEEN a.valid_from AND a.valid_until),
		COALESCE(d.verification_status,'legacy_unverified'),COALESCE(d.inn,''),COALESCE(d.ogrn,''),
		COALESCE(d.license_number,''),COALESCE(d.license_status,'unknown'),COALESCE(d.institution_status,'unknown')
		FROM partners p
		LEFT JOIN education_directory d ON d.id=p.directory_id
		LEFT JOIN LATERAL (
		 SELECT a.number,a.signed_on FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id
		 WHERE ap.partner_id=p.id ORDER BY (a.status='active') DESC,a.valid_from DESC,a.created_at DESC LIMIT 1
		) latest ON true
		WHERE ($1='' OR p.id::text=$1) ORDER BY p.name,p.id`+page, partnerScope(u, ""))
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	partners := make([]models.Partner, 0)
	for rows.Next() {
		var p models.Partner
		var agreementDate sql.NullTime
		var otherAgreement sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.PartnerKind, &p.DirectoryID, &p.AgreementNumber,
			&agreementDate, &otherAgreement, &p.CreatedAt, &p.AgreementsCount, &p.ActiveAgreementsCount,
			&p.VerificationStatus, &p.INN, &p.OGRN, &p.LicenseNumber, &p.LicenseStatus, &p.InstitutionStatus); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		p.OtherAgreement = otherAgreement.String
		if agreementDate.Valid {
			date := agreementDate.Time.Format("2006-01-02")
			p.AgreementDate = &date
		}
		partners = append(partners, p)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения организаций")
		return
	}
	writePage(w, r, partners)
}

type createPartnerRequest struct {
	DirectoryID      string                 `json:"directory_id"`
	InitialAgreement *agreementWriteRequest `json:"initial_agreement"`
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
	req.DirectoryID = strings.TrimSpace(req.DirectoryID)
	if req.DirectoryID == "" {
		middleware.WriteError(w, 400, "выберите организацию из проверенного справочника")
		return
	}
	if req.InitialAgreement == nil {
		middleware.WriteError(w, 400, "первое соглашение обязательно")
		return
	}
	// The new partner id is not known yet; callers cannot choose a different
	// organization for the initial agreement.
	req.InitialAgreement.PartnerIDs = []string{"new-partner"}
	agreement, err := normalizeAgreement(*req.InitialAgreement)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var name, partnerKind string
	var selectable bool
	err = tx.QueryRowContext(r.Context(), `SELECT name,partner_kind,
		(verification_status='verified' AND license_status='active' AND institution_status='active'
		 AND verified_at>=now()-interval '35 days'
		 AND registry_updated_at BETWEEN CURRENT_DATE-35 AND CURRENT_DATE)
		FROM education_directory WHERE id::text=$1 FOR SHARE`, req.DirectoryID).
		Scan(&name, &partnerKind, &selectable)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 400, "организация не найдена в справочнике")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка проверки справочника")
		return
	}
	if !selectable {
		middleware.WriteError(w, 409, "организация не подтверждена действующей лицензией; обновите официальный справочник")
		return
	}
	var partnerID string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO partners(name,partner_kind,directory_id,agreement_date,agreement_number)
		VALUES($1,$2,$3,$4,$5) RETURNING id`, name, partnerKind, req.DirectoryID, agreement.SignedOn, agreement.Request.Number).Scan(&partnerID)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "организация уже добавлена в партнёры")
		return
	}
	agreement.Request.PartnerIDs = []string{partnerID}
	agreementID, err := insertAgreement(r.Context(), tx, agreement, u.ID)
	if err != nil {
		middleware.WriteError(w, 409, "не удалось сохранить первое соглашение")
		return
	}
	if err := logAudit(tx, "partner", partnerID, "create", u.ID, "", nil, map[string]interface{}{
		"directory_id": req.DirectoryID, "agreement_id": agreementID,
	}); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err := tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": partnerID, "agreement_id": agreementID})
}
