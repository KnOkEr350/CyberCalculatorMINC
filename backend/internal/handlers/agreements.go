package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"github.com/lib/pq"
)

type AgreementHandlers struct {
	DB *sql.DB
}

type agreementWriteRequest struct {
	PartnerIDs          []string                            `json:"partner_ids"`
	AgreementKind       string                              `json:"agreement_kind"`
	Number              string                              `json:"number"`
	Status              string                              `json:"status"`
	SignedOn            string                              `json:"signed_on"`
	ValidFrom           string                              `json:"valid_from"`
	ValidUntil          string                              `json:"valid_until"`
	RegionalAuthorityID string                              `json:"regional_authority_id,omitempty"`
	ROIVName            string                              `json:"roiv_name,omitempty"`
	LegalEntityGroup    string                              `json:"legal_entity_group,omitempty"`
	SignatureMethod     string                              `json:"signature_method"`
	SignedBy            string                              `json:"signed_by,omitempty"`
	SignatureDate       string                              `json:"signature_date,omitempty"`
	DocumentReference   string                              `json:"document_reference,omitempty"`
	Notes               string                              `json:"notes,omitempty"`
	ResponsiblePeople   []models.AgreementResponsiblePerson `json:"responsible_people"`
}

type normalizedAgreement struct {
	Request       agreementWriteRequest
	SignedOn      time.Time
	ValidFrom     time.Time
	ValidUntil    time.Time
	SignatureDate interface{}
}

func normalizeAgreement(req agreementWriteRequest) (normalizedAgreement, error) {
	trim := func(value string, limit int, label string) (string, error) {
		value = strings.Join(strings.Fields(value), " ")
		if len([]rune(value)) > limit {
			return "", fmt.Errorf("%s: не более %d символов", label, limit)
		}
		return value, nil
	}
	var err error
	if req.Number, err = trim(req.Number, 100, "номер соглашения"); err != nil || req.Number == "" {
		if err != nil {
			return normalizedAgreement{}, err
		}
		return normalizedAgreement{}, fmt.Errorf("номер соглашения обязателен")
	}
	if req.ROIVName, err = trim(req.ROIVName, 300, "наименование РОИВ"); err != nil {
		return normalizedAgreement{}, err
	}
	req.RegionalAuthorityID = strings.TrimSpace(req.RegionalAuthorityID)
	if req.LegalEntityGroup, err = trim(req.LegalEntityGroup, 1000, "группа юридических лиц"); err != nil {
		return normalizedAgreement{}, err
	}
	if req.SignedBy, err = trim(req.SignedBy, 300, "подписант"); err != nil {
		return normalizedAgreement{}, err
	}
	if req.DocumentReference, err = trim(req.DocumentReference, 1000, "ссылка на документ"); err != nil {
		return normalizedAgreement{}, err
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if len([]rune(req.Notes)) > 1000 {
		return normalizedAgreement{}, fmt.Errorf("примечание: не более 1000 символов")
	}
	if req.AgreementKind != "education_organization" && req.AgreementKind != "roiv" {
		return normalizedAgreement{}, fmt.Errorf("тип соглашения должен быть education_organization или roiv")
	}
	if req.AgreementKind == "roiv" && req.RegionalAuthorityID == "" {
		return normalizedAgreement{}, fmt.Errorf("для соглашения с РОИВ выберите региональный орган управления образованием")
	}
	if req.AgreementKind == "education_organization" && req.RegionalAuthorityID != "" {
		return normalizedAgreement{}, fmt.Errorf("РОИВ можно выбрать только для соглашения с региональным органом")
	}
	validStatuses := map[string]bool{"draft": true, "active": true, "suspended": true, "expired": true, "terminated": true}
	if !validStatuses[req.Status] {
		return normalizedAgreement{}, fmt.Errorf("некорректный статус соглашения")
	}
	validSignatures := map[string]bool{"unsigned": true, "paper": true, "qualified_electronic": true, "goskey": true}
	if !validSignatures[req.SignatureMethod] {
		return normalizedAgreement{}, fmt.Errorf("некорректный способ подписания")
	}
	parseDate := func(value, label string) (time.Time, error) {
		date, parseErr := time.Parse("2006-01-02", value)
		if parseErr != nil {
			return time.Time{}, fmt.Errorf("%s обязательна и должна быть в формате ГГГГ-ММ-ДД", label)
		}
		return date, nil
	}
	signedOn, err := parseDate(req.SignedOn, "дата соглашения")
	if err != nil {
		return normalizedAgreement{}, err
	}
	validFrom, err := parseDate(req.ValidFrom, "дата начала действия")
	if err != nil {
		return normalizedAgreement{}, err
	}
	validUntil, err := parseDate(req.ValidUntil, "дата окончания действия")
	if err != nil {
		return normalizedAgreement{}, err
	}
	if validUntil.Before(validFrom) {
		return normalizedAgreement{}, fmt.Errorf("дата окончания не может быть раньше даты начала")
	}
	var signatureDate interface{}
	if req.SignatureDate != "" {
		parsed, parseErr := parseDate(req.SignatureDate, "дата подписания")
		if parseErr != nil {
			return normalizedAgreement{}, parseErr
		}
		signatureDate = parsed
	}
	if req.Status == "active" && (req.SignatureMethod == "unsigned" || req.SignedBy == "" || signatureDate == nil) {
		return normalizedAgreement{}, fmt.Errorf("для действующего соглашения укажите способ, дату и подписанта")
	}
	if len(req.PartnerIDs) == 0 {
		return normalizedAgreement{}, fmt.Errorf("выберите хотя бы одно учебное заведение")
	}
	seenPartners := make(map[string]bool, len(req.PartnerIDs))
	partnerIDs := make([]string, 0, len(req.PartnerIDs))
	for _, id := range req.PartnerIDs {
		id = strings.TrimSpace(id)
		if id == "" || seenPartners[id] {
			return normalizedAgreement{}, fmt.Errorf("список учебных заведений содержит пустое значение или дубль")
		}
		seenPartners[id] = true
		partnerIDs = append(partnerIDs, id)
	}
	req.PartnerIDs = partnerIDs
	if len(req.ResponsiblePeople) > 20 {
		return normalizedAgreement{}, fmt.Errorf("не более 20 ответственных лиц в одном соглашении")
	}
	parties := map[string]bool{}
	for i := range req.ResponsiblePeople {
		person := &req.ResponsiblePeople[i]
		person.FullName = strings.Join(strings.Fields(person.FullName), " ")
		person.Position = strings.TrimSpace(person.Position)
		person.Email = strings.ToLower(strings.TrimSpace(person.Email))
		person.Phone = strings.TrimSpace(person.Phone)
		if person.Party != "cyberprotect" && person.Party != "counterparty" {
			return normalizedAgreement{}, fmt.Errorf("ответственное лицо %d: выберите сторону", i+1)
		}
		parties[person.Party] = true
		if len([]rune(person.FullName)) < 2 || len([]rune(person.FullName)) > 200 {
			return normalizedAgreement{}, fmt.Errorf("ответственное лицо %d: укажите ФИО", i+1)
		}
		if len([]rune(person.Position)) > 200 || len([]rune(person.Phone)) > 40 {
			return normalizedAgreement{}, fmt.Errorf("ответственное лицо %d: должность или телефон слишком длинные", i+1)
		}
		if person.Email != "" {
			address, mailErr := mail.ParseAddress(person.Email)
			if mailErr != nil || address.Address != person.Email || len(person.Email) > 254 {
				return normalizedAgreement{}, fmt.Errorf("ответственное лицо %d: некорректный email", i+1)
			}
		}
	}
	if req.Status == "active" && (!parties["cyberprotect"] || !parties["counterparty"]) {
		return normalizedAgreement{}, fmt.Errorf("для действующего соглашения укажите ответственных лиц обеих сторон")
	}
	return normalizedAgreement{Request: req, SignedOn: signedOn, ValidFrom: validFrom, ValidUntil: validUntil, SignatureDate: signatureDate}, nil
}

func insertAgreement(ctx context.Context, tx *sql.Tx, agreement normalizedAgreement, userID string) (string, error) {
	r := agreement.Request
	var id string
	err := tx.QueryRowContext(ctx, `INSERT INTO agreements(
		agreement_kind,number,status,signed_on,valid_from,valid_until,regional_authority_id,roiv_name,legal_entity_group,
		signature_method,signed_by,signature_date,document_reference,notes,created_by,updated_by)
		VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,''),NULLIF($9,''),$10,NULLIF($11,''),$12,NULLIF($13,''),NULLIF($14,''),$15,$15)
		RETURNING id`, r.AgreementKind, r.Number, r.Status, agreement.SignedOn, agreement.ValidFrom,
		agreement.ValidUntil, r.RegionalAuthorityID, r.ROIVName, r.LegalEntityGroup, r.SignatureMethod,
		r.SignedBy, agreement.SignatureDate, r.DocumentReference, r.Notes, userID).Scan(&id)
	if err != nil {
		return "", err
	}
	for i, partnerID := range r.PartnerIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO agreement_partners(agreement_id,partner_id,is_primary) VALUES($1,$2,$3)`, id, partnerID, i == 0); err != nil {
			return "", err
		}
	}
	for _, person := range r.ResponsiblePeople {
		if _, err = tx.ExecContext(ctx, `INSERT INTO agreement_responsible_people(agreement_id,party,full_name,position,email,phone)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''))`, id, person.Party, person.FullName, person.Position, person.Email, person.Phone); err != nil {
			return "", err
		}
	}
	return id, nil
}

func ensureAgreementPartners(ctx context.Context, tx *sql.Tx, partnerIDs []string) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM partners WHERE id::text=ANY($1)`, pq.Array(partnerIDs)).Scan(&count); err != nil {
		return err
	}
	if count != len(partnerIDs) {
		return fmt.Errorf("одно или несколько учебных заведений не найдены")
	}
	return nil
}

func prepareAgreementRelations(ctx context.Context, tx *sql.Tx, agreement *normalizedAgreement) error {
	if err := ensureAgreementPartners(ctx, tx, agreement.Request.PartnerIDs); err != nil {
		return err
	}
	var schools int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE partner_kind='school')
		FROM partners WHERE id::text=ANY($1)`, pq.Array(agreement.Request.PartnerIDs)).Scan(&schools); err != nil {
		return err
	}
	if agreement.Request.AgreementKind == "roiv" {
		if schools != len(agreement.Request.PartnerIDs) {
			return fmt.Errorf("соглашение с РОИВ может охватывать только школы")
		}
		var name, status string
		if err := tx.QueryRowContext(ctx, `SELECT name,status FROM regional_authorities WHERE id::text=$1`, agreement.Request.RegionalAuthorityID).Scan(&name, &status); err == sql.ErrNoRows {
			return fmt.Errorf("выбранный РОИВ отсутствует в справочнике")
		} else if err != nil {
			return err
		}
		if status != "active" {
			return fmt.Errorf("выбранный РОИВ не действует")
		}
		agreement.Request.ROIVName = name
		return nil
	}
	if schools > 0 {
		return fmt.Errorf("для школы требуется соглашение с РОИВ")
	}
	agreement.Request.ROIVName = ""
	return nil
}

func (h *AgreementHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	requestedPartner := strings.TrimSpace(r.URL.Query().Get("partner_id"))
	if requestedPartner != "" && !requirePartner(w, u, requestedPartner) {
		return
	}
	scope := partnerScope(u, requestedPartner)
	rows, err := h.DB.QueryContext(r.Context(), `SELECT a.id,a.agreement_kind,a.number,a.status,a.signed_on,a.valid_from,a.valid_until,
		COALESCE(a.regional_authority_id::text,''),COALESCE(ra.name,a.roiv_name,''),COALESCE(a.legal_entity_group,''),a.signature_method,COALESCE(a.signed_by,''),
		COALESCE(a.signature_date::text,''),COALESCE(a.document_reference,''),COALESCE(a.notes,''),
		a.created_at,a.updated_at,array_agg(ap.partner_id::text ORDER BY ap.is_primary DESC,ap.partner_id)
		FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id
		LEFT JOIN regional_authorities ra ON ra.id=a.regional_authority_id
		WHERE ($1='' OR EXISTS(SELECT 1 FROM agreement_partners access
		 WHERE access.agreement_id=a.id AND access.partner_id::text=$1))
		GROUP BY a.id,ra.name ORDER BY a.valid_from DESC,a.number,a.id`+page, scope)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса соглашений")
		return
	}
	defer rows.Close()
	out := make([]models.Agreement, 0)
	for rows.Next() {
		var agreement models.Agreement
		var signedOn, validFrom, validUntil time.Time
		var partnerIDs pq.StringArray
		if err = rows.Scan(&agreement.ID, &agreement.AgreementKind, &agreement.Number, &agreement.Status,
			&signedOn, &validFrom, &validUntil, &agreement.RegionalAuthorityID, &agreement.ROIVName, &agreement.LegalEntityGroup,
			&agreement.SignatureMethod, &agreement.SignedBy, &agreement.SignatureDate,
			&agreement.DocumentReference, &agreement.Notes, &agreement.CreatedAt, &agreement.UpdatedAt, &partnerIDs); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения соглашений")
			return
		}
		agreement.SignedOn = signedOn.Format("2006-01-02")
		agreement.ValidFrom = validFrom.Format("2006-01-02")
		agreement.ValidUntil = validUntil.Format("2006-01-02")
		agreement.PartnerIDs = []string(partnerIDs)
		people, peopleErr := h.people(r.Context(), agreement.ID)
		if peopleErr != nil {
			middleware.WriteError(w, 500, "ошибка чтения ответственных лиц")
			return
		}
		agreement.ResponsiblePeople = people
		out = append(out, agreement)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения соглашений")
		return
	}
	writePage(w, r, out)
}

func (h *AgreementHandlers) people(ctx context.Context, agreementID string) ([]models.AgreementResponsiblePerson, error) {
	rows, err := h.DB.QueryContext(ctx, `SELECT id,party,full_name,COALESCE(position,''),COALESCE(email,''),COALESCE(phone,'')
		FROM agreement_responsible_people WHERE agreement_id=$1 ORDER BY party,full_name,id`, agreementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AgreementResponsiblePerson, 0)
	for rows.Next() {
		var person models.AgreementResponsiblePerson
		if err = rows.Scan(&person.ID, &person.Party, &person.FullName, &person.Position, &person.Email, &person.Phone); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

func (h *AgreementHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "соглашения ведёт сотрудник Киберпротекта")
		return
	}
	var req agreementWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	agreement, err := normalizeAgreement(req)
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
	if err = prepareAgreementRelations(r.Context(), tx, &agreement); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	id, err := insertAgreement(r.Context(), tx, agreement, u.ID)
	if err != nil {
		middleware.WriteError(w, 409, "не удалось сохранить соглашение")
		return
	}
	if logAudit(tx, "agreement", id, "create", u.ID, "", nil, agreement.Request) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения соглашения")
		return
	}
	middleware.WriteJSON(w, 201, map[string]string{"id": id})
}

func (h *AgreementHandlers) Update(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, agreementID string) {
	if !isStaff(u) {
		middleware.WriteError(w, 403, "соглашения ведёт сотрудник Киберпротекта")
		return
	}
	var req agreementWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	agreement, err := normalizeAgreement(req)
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
	var exists bool
	if err = tx.QueryRowContext(r.Context(), `SELECT true FROM agreements WHERE id::text=$1 FOR UPDATE`, agreementID).Scan(&exists); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "соглашение не найдено")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка запроса")
		return
	}
	if err = prepareAgreementRelations(r.Context(), tx, &agreement); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	rq := agreement.Request
	_, err = tx.ExecContext(r.Context(), `UPDATE agreements SET agreement_kind=$1,number=$2,status=$3,signed_on=$4,
		valid_from=$5,valid_until=$6,regional_authority_id=NULLIF($7,'')::uuid,roiv_name=NULLIF($8,''),
		legal_entity_group=NULLIF($9,''),signature_method=$10,signed_by=NULLIF($11,''),signature_date=$12,
		document_reference=NULLIF($13,''),notes=NULLIF($14,''),updated_by=$15,updated_at=now()
		WHERE id::text=$16`, rq.AgreementKind, rq.Number, rq.Status, agreement.SignedOn, agreement.ValidFrom,
		agreement.ValidUntil, rq.RegionalAuthorityID, rq.ROIVName, rq.LegalEntityGroup, rq.SignatureMethod,
		rq.SignedBy, agreement.SignatureDate, rq.DocumentReference, rq.Notes, u.ID, agreementID)
	if err != nil {
		middleware.WriteError(w, 409, "не удалось изменить соглашение")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM agreement_responsible_people WHERE agreement_id=$1`, agreementID); err != nil {
		middleware.WriteError(w, 500, "ошибка обновления ответственных лиц")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE agreement_partners SET is_primary=false WHERE agreement_id=$1`, agreementID); err != nil {
		middleware.WriteError(w, 500, "ошибка обновления состава организаций")
		return
	}
	for i, partnerID := range rq.PartnerIDs {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO agreement_partners(agreement_id,partner_id,is_primary) VALUES($1,$2,$3)
			ON CONFLICT(agreement_id,partner_id) DO UPDATE SET is_primary=EXCLUDED.is_primary`, agreementID, partnerID, i == 0); err != nil {
			middleware.WriteError(w, 409, "не удалось обновить состав организаций")
			return
		}
	}
	// Removing a partner already used by an entry is intentionally rejected by
	// the composite foreign key instead of silently detaching historical data.
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM agreement_partners WHERE agreement_id=$1 AND NOT(partner_id::text=ANY($2))`, agreementID, pq.Array(rq.PartnerIDs)); err != nil {
		middleware.WriteError(w, 409, "нельзя исключить организацию: по соглашению уже есть записи")
		return
	}
	for _, person := range rq.ResponsiblePeople {
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO agreement_responsible_people(agreement_id,party,full_name,position,email,phone)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''))`, agreementID, person.Party, person.FullName, person.Position, person.Email, person.Phone); err != nil {
			middleware.WriteError(w, 500, "ошибка обновления ответственных лиц")
			return
		}
	}
	if logAudit(tx, "agreement", agreementID, "update", u.ID, "", nil, rq) != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения соглашения")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": agreementID})
}
