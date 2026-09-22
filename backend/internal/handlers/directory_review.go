package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"github.com/lib/pq"
)

type directoryWriteRequest struct {
	Name                string    `json:"name"`
	PartnerKind         string    `json:"partner_kind"`
	Region              string    `json:"region"`
	INN                 string    `json:"inn"`
	OGRN                string    `json:"ogrn"`
	LicenseNumber       string    `json:"license_number"`
	LicenseStatus       string    `json:"license_status"`
	InstitutionStatus   string    `json:"institution_status"`
	SourceURL           string    `json:"source_url"`
	RegistryUpdatedAt   string    `json:"registry_updated_at"`
	Confirm             bool      `json:"confirm"`
	ConfirmationComment string    `json:"confirmation_comment"`
	ProgramCodes        *[]string `json:"program_codes"`
	ProgramsSourceURL   string    `json:"programs_source_url"`
	ProposalComment     string    `json:"proposal_comment"`
}

const ministryEducationDirectoryURL = "https://adm.digital.gov.ru/app/uploads/2026/05/6027f0_perechen-oo-vo-realizuyushhih-it-speczialnosti.pdf"

func normalizeDirectoryWrite(req *directoryWriteRequest) error {
	req.Name = strings.Join(strings.Fields(req.Name), " ")
	req.PartnerKind = strings.TrimSpace(req.PartnerKind)
	req.Region = strings.Join(strings.Fields(req.Region), " ")
	req.INN = strings.TrimSpace(req.INN)
	req.OGRN = strings.TrimSpace(req.OGRN)
	req.LicenseNumber = strings.TrimSpace(req.LicenseNumber)
	req.LicenseStatus = strings.TrimSpace(req.LicenseStatus)
	req.InstitutionStatus = strings.TrimSpace(req.InstitutionStatus)
	req.SourceURL = strings.TrimSpace(req.SourceURL)
	req.RegistryUpdatedAt = strings.TrimSpace(req.RegistryUpdatedAt)
	req.ConfirmationComment = strings.TrimSpace(req.ConfirmationComment)
	req.ProposalComment = strings.TrimSpace(req.ProposalComment)
	if req.ProgramCodes != nil {
		codes, err := normalizeProgramCodes(*req.ProgramCodes)
		if err != nil {
			return err
		}
		req.ProgramCodes = &codes
		req.ProgramsSourceURL = strings.TrimSpace(req.ProgramsSourceURL)
		if len(codes) > 0 && !publicProgramURL(req.ProgramsSourceURL) {
			return fmt.Errorf("укажите HTTPS-ссылку на страницу образовательных программ вуза или реестра")
		}
	}

	if utf8.RuneCountInString(req.Name) < 2 || utf8.RuneCountInString(req.Name) > 1000 {
		return fmt.Errorf("наименование должно содержать от 2 до 1000 символов")
	}
	if req.PartnerKind != "vuz" && req.PartnerKind != "kolledj" && req.PartnerKind != "school" {
		return fmt.Errorf("выберите тип учебного заведения")
	}
	if utf8.RuneCountInString(req.Region) < 2 || utf8.RuneCountInString(req.Region) > 200 {
		return fmt.Errorf("регион должен содержать от 2 до 200 символов")
	}
	if req.INN != "" && !validINN(req.INN) {
		return fmt.Errorf("ИНН не прошёл проверку контрольной суммы")
	}
	if req.OGRN != "" && !validOGRN(req.OGRN) {
		return fmt.Errorf("ОГРН / ОГРНИП не прошёл проверку контрольной суммы")
	}
	if req.Confirm && (req.INN == "" || req.OGRN == "") {
		return fmt.Errorf("для подтверждения обязательны ИНН и ОГРН / ОГРНИП")
	}
	if utf8.RuneCountInString(req.LicenseNumber) > 100 {
		return fmt.Errorf("номер лицензии не должен превышать 100 символов")
	}
	if req.LicenseStatus == "" {
		req.LicenseStatus = "unknown"
	}
	if req.InstitutionStatus == "" {
		req.InstitutionStatus = "unknown"
	}
	if !oneOf(req.LicenseStatus, "active", "suspended", "expired", "revoked", "unknown") {
		return fmt.Errorf("некорректный статус лицензии")
	}
	if !oneOf(req.InstitutionStatus, "active", "inactive", "reorganized", "liquidated", "unknown") {
		return fmt.Errorf("некорректный статус организации")
	}
	if req.SourceURL != "" && !officialRegistryURL(req.SourceURL) {
		return fmt.Errorf("укажите HTTPS-ссылку официального источника")
	}
	if req.Confirm && req.SourceURL == "" {
		return fmt.Errorf("для подтверждения укажите официальный источник")
	}
	if req.RegistryUpdatedAt != "" {
		date, err := time.Parse("2006-01-02", req.RegistryUpdatedAt)
		moscowToday := time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01-02")
		if err != nil || req.RegistryUpdatedAt > moscowToday {
			return fmt.Errorf("дата актуальности должна быть корректной и не из будущего")
		}
		if req.Confirm && !freshRegistryDate(date, time.Now()) {
			return fmt.Errorf("для подтверждения сведения должны быть проверены не более 35 дней назад")
		}
	} else if req.Confirm {
		return fmt.Errorf("для подтверждения укажите дату актуальности сведений")
	}
	if utf8.RuneCountInString(req.ConfirmationComment) > 1000 {
		return fmt.Errorf("комментарий не должен превышать 1000 символов")
	}
	if utf8.RuneCountInString(req.ProposalComment) > 1000 {
		return fmt.Errorf("обоснование предложения не должно превышать 1000 символов")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func directoryAuditValue(name, kind, region, inn, ogrn, licenseNumber, licenseStatus, institutionStatus, recordID, sourceURL, updatedAt, verificationStatus string) map[string]string {
	return map[string]string{
		"name": name, "partner_kind": kind, "region": region, "inn": inn, "ogrn": ogrn,
		"license_number": licenseNumber, "license_status": licenseStatus,
		"institution_status": institutionStatus, "registry_record_id": recordID,
		"source_url": sourceURL, "registry_updated_at": updatedAt,
		"verification_status": verificationStatus,
	}
}

func (h *PartnerHandlers) UpdateDirectory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canApproveEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "изменять и подтверждать учебные заведения может только администратор")
		return
	}
	var req directoryWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if err := normalizeDirectoryWrite(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()

	var name, kind, region, inn, ogrn, licenseNumber, licenseStatus, institutionStatus string
	var recordID, sourceURL, updatedAt, verificationStatus string
	err = tx.QueryRowContext(r.Context(), `SELECT name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
		COALESCE(license_number,''),license_status,institution_status,COALESCE(registry_record_id,''),
		COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),verification_status
		FROM education_directory WHERE id::text=$1 FOR UPDATE`, id).
		Scan(&name, &kind, &region, &inn, &ogrn, &licenseNumber, &licenseStatus, &institutionStatus,
			&recordID, &sourceURL, &updatedAt, &verificationStatus)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, http.StatusNotFound, "учебное заведение не найдено")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения справочника")
		return
	}
	oldValue := directoryAuditValue(name, kind, region, inn, ogrn, licenseNumber, licenseStatus,
		institutionStatus, recordID, sourceURL, updatedAt, verificationStatus)
	var oldCodes pq.StringArray
	var oldProgramsSource string
	if err = tx.QueryRowContext(r.Context(), `SELECT program_codes,programs_source_url FROM education_directory WHERE id::text=$1`, id).Scan(&oldCodes, &oldProgramsSource); err != nil {
		middleware.WriteError(w, 500, "ошибка чтения направлений")
		return
	}
	oldValue["program_codes"], oldValue["programs_source_url"] = strings.Join(oldCodes, ","), oldProgramsSource
	if req.Confirm {
		effectiveCodes := []string(oldCodes)
		if req.ProgramCodes != nil {
			effectiveCodes = *req.ProgramCodes
		}
		var matchesOrder bool
		if err = tx.QueryRowContext(r.Context(), `SELECT education_matches_order($1,$2)`, req.PartnerKind, pq.Array(effectiveCodes)).Scan(&matchesOrder); err != nil {
			middleware.WriteError(w, 500, "не удалось проверить направления подготовки")
			return
		}
		if !matchesOrder {
			middleware.WriteError(w, http.StatusBadRequest, "для подтверждения вуза укажите хотя бы одно направление из перечня Минцифры")
			return
		}
	}

	status := "pending"
	var verifier interface{}
	if req.Confirm {
		status = "verified"
		verifier = u.ID
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE education_directory SET name=$1,partner_kind=$2,region=$3,
		inn=NULLIF($4,''),ogrn=NULLIF($5,''),license_number=NULLIF($6,''),license_status=$7,
		institution_status=$8,source_url=NULLIF($9,''),registry_updated_at=NULLIF($10,'')::date,
		verification_status=$11,verified_at=CASE WHEN $11='verified' THEN now() ELSE NULL END,
		verified_by=$12,updated_at=now() WHERE id::text=$13`,
		req.Name, req.PartnerKind, req.Region, req.INN, req.OGRN, req.LicenseNumber,
		req.LicenseStatus, req.InstitutionStatus, req.SourceURL, req.RegistryUpdatedAt,
		status, verifier, id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "не удалось сохранить: проверьте уникальность записи")
		return
	}
	if req.ProgramCodes != nil {
		if _, err = tx.ExecContext(r.Context(), `UPDATE education_directory SET program_codes=$2,programs_source_url=$3,programs_checked_at=now() WHERE id::text=$1`, id, pq.Array(*req.ProgramCodes), req.ProgramsSourceURL); err != nil {
			middleware.WriteError(w, 500, "не удалось сохранить направления")
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE partners SET name=$1,partner_kind=$2 WHERE directory_id::text=$3`, req.Name, req.PartnerKind, id); err != nil {
		middleware.WriteError(w, 500, "не удалось обновить связанного партнёра")
		return
	}
	newValue := directoryAuditValue(req.Name, req.PartnerKind, req.Region, req.INN, req.OGRN,
		req.LicenseNumber, req.LicenseStatus, req.InstitutionStatus, recordID, req.SourceURL,
		req.RegistryUpdatedAt, status)
	newValue["program_codes"], newValue["programs_source_url"] = strings.Join(oldCodes, ","), oldProgramsSource
	if req.ProgramCodes != nil {
		newValue["program_codes"], newValue["programs_source_url"] = strings.Join(*req.ProgramCodes, ","), req.ProgramsSourceURL
	}
	action := "directory_update"
	comment := "Реквизиты изменены; требуется подтверждение"
	if req.Confirm {
		action = "directory_confirm"
		comment = "Учебное заведение подтверждено"
		if _, err = tx.ExecContext(r.Context(), `UPDATE education_directory SET reviewed_by=$1,reviewed_at=now(),review_comment=$2
			WHERE id::text=$3 AND proposed_by IS NOT NULL`, u.ID, req.ConfirmationComment, id); err != nil {
			middleware.WriteError(w, 500, "ошибка подтверждения предложения")
			return
		}
	}
	if req.ConfirmationComment != "" {
		comment += ": " + req.ConfirmationComment
	}
	if err = logAudit(r.Context(), tx, "education_directory", id, action, u.ID, comment, oldValue, newValue); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err = tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"id": id, "verification_status": status})
}

func (h *PartnerHandlers) CreateDirectory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canProposeEducationDirectory(u) && !canApproveEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "добавлять учебные заведения могут администратор и модератор")
		return
	}
	var req directoryWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	if canProposeEducationDirectory(u) {
		req.Confirm = false
		if utf8.RuneCountInString(strings.TrimSpace(req.ProposalComment)) < 5 {
			middleware.WriteError(w, http.StatusBadRequest, "кратко объясните, почему организацию нужно добавить")
			return
		}
	}
	if err := normalizeDirectoryWrite(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.SourceURL == "" {
		middleware.WriteError(w, http.StatusBadRequest, "укажите официальный источник сведений")
		return
	}
	if req.PartnerKind == "vuz" && (req.ProgramCodes == nil || len(*req.ProgramCodes) == 0) {
		middleware.WriteError(w, http.StatusBadRequest, "для вуза укажите хотя бы одно ИТ-направление")
		return
	}
	if req.Confirm && (req.LicenseStatus != "active" || req.InstitutionStatus != "active") {
		middleware.WriteError(w, http.StatusBadRequest, "сразу подтвердить можно только действующую организацию с действующей лицензией")
		return
	}

	status := "pending"
	var verifiedBy, proposedBy interface{}
	if req.Confirm {
		status, verifiedBy = "verified", u.ID
	}
	if canProposeEducationDirectory(u) {
		proposedBy = u.ID
	}
	codes := []string{}
	if req.ProgramCodes != nil {
		codes = *req.ProgramCodes
	}
	sourceLabel := "Добавлено администратором"
	if proposedBy != nil {
		sourceLabel = "Предложено модератором"
	}
	if req.SourceURL == ministryEducationDirectoryURL {
		sourceLabel = "Перечень образовательных организаций Минцифры России, 2026"
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	if req.Confirm {
		var matchesOrder bool
		if err = tx.QueryRowContext(r.Context(), `SELECT education_matches_order($1,$2)`, req.PartnerKind, pq.Array(codes)).Scan(&matchesOrder); err != nil {
			middleware.WriteError(w, 500, "не удалось проверить направления подготовки")
			return
		}
		if !matchesOrder {
			middleware.WriteError(w, http.StatusBadRequest, "для подтверждения вуза укажите хотя бы одно направление из перечня Минцифры")
			return
		}
	}
	var id string
	err = tx.QueryRowContext(r.Context(), `WITH generated AS (SELECT gen_random_uuid() AS id)
		INSERT INTO education_directory(id,name,partner_kind,region,source,inn,ogrn,license_number,
			license_status,institution_status,registry_record_id,source_url,registry_updated_at,
			verification_status,verified_at,verified_by,program_codes,programs_source_url,programs_checked_at,
			proposed_by,proposal_comment,proposed_at,listed_in_mincifry_order_27)
		SELECT id,$1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),NULLIF($7,''),$8,$9,'manual:'||id::text,
			$10,NULLIF($11,'')::date,$12,CASE WHEN $12='verified' THEN now() END,$13,$14,$15,
			CASE WHEN cardinality($14::text[])>0 THEN now() END,$16,NULLIF($17,''),CASE WHEN $16::uuid IS NOT NULL THEN now() END,$18
		FROM generated RETURNING id::text`, req.Name, req.PartnerKind, req.Region, sourceLabel, req.INN, req.OGRN,
		req.LicenseNumber, req.LicenseStatus, req.InstitutionStatus, req.SourceURL, req.RegistryUpdatedAt,
		status, verifiedBy, pq.Array(codes), req.ProgramsSourceURL, proposedBy, req.ProposalComment,
		req.PartnerKind == "vuz" && req.SourceURL == ministryEducationDirectoryURL).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "организация с такими реквизитами уже существует")
		return
	}
	action, comment := "directory_create", "Учебное заведение добавлено администратором"
	if proposedBy != nil {
		action, comment = "directory_propose", "Модератор предложил добавить учебное заведение: "+req.ProposalComment
	}
	if err = logAudit(r.Context(), tx, "education_directory", id, action, u.ID, comment, nil, map[string]interface{}{
		"name": req.Name, "source_url": req.SourceURL, "verification_status": status,
	}); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "не удалось сохранить учебное заведение")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id, "verification_status": status})
}

func (h *PartnerHandlers) DirectoryProposals(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canProposeEducationDirectory(u) && !canApproveEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к предложениям справочника")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT d.id::text,d.name,d.partner_kind,d.region,
		COALESCE(d.inn,''),COALESCE(d.ogrn,''),COALESCE(d.license_number,''),d.license_status,d.institution_status,
		d.verification_status,COALESCE(d.proposal_comment,''),
		d.proposed_at,submitter.full_name,submitter.email,COALESCE(d.review_comment,''),d.reviewed_at,
		COALESCE(d.source_url,''),COALESCE(d.registry_updated_at::text,''),d.program_codes,d.programs_source_url
		FROM education_directory d JOIN users submitter ON submitter.id=d.proposed_by
		WHERE ($1 OR d.proposed_by::text=$2)
		ORDER BY (d.verification_status='pending') DESC,d.proposed_at DESC,d.id DESC LIMIT 200`,
		canApproveEducationDirectory(u), u.ID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось загрузить предложения")
		return
	}
	defer rows.Close()
	out := []map[string]interface{}{}
	for rows.Next() {
		var id, name, kind, region, inn, ogrn, licenseNumber, licenseStatus, institutionStatus string
		var status, comment, submitter, email, reviewComment, sourceURL, registryUpdatedAt, programsSourceURL string
		var proposedAt time.Time
		var reviewedAt sql.NullTime
		var codes pq.StringArray
		if err = rows.Scan(&id, &name, &kind, &region, &inn, &ogrn, &licenseNumber, &licenseStatus,
			&institutionStatus, &status, &comment, &proposedAt, &submitter, &email, &reviewComment,
			&reviewedAt, &sourceURL, &registryUpdatedAt, &codes, &programsSourceURL); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения предложений")
			return
		}
		reviewed := ""
		if reviewedAt.Valid {
			reviewed = reviewedAt.Time.Format(time.RFC3339)
		}
		out = append(out, map[string]interface{}{"id": id, "name": name, "partner_kind": kind, "region": region,
			"inn": inn, "ogrn": ogrn, "license_number": licenseNumber, "license_status": licenseStatus,
			"institution_status": institutionStatus, "verification_status": status, "proposal_comment": comment,
			"proposed_at": proposedAt.Format(time.RFC3339), "submitter_name": submitter, "submitter_email": email,
			"review_comment": reviewComment, "reviewed_at": reviewed, "source_url": sourceURL,
			"registry_updated_at": registryUpdatedAt, "program_codes": codes, "programs_source_url": programsSourceURL})
	}
	if err = rows.Err(); err != nil {
		middleware.WriteError(w, 500, "ошибка чтения предложений")
		return
	}
	middleware.WriteJSON(w, 200, out)
}

func (h *PartnerHandlers) DecideDirectoryProposal(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	if !canApproveEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "решение по предложению принимает только администратор")
		return
	}
	var req struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
	}
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, 400, "некорректный запрос")
		return
	}
	req.Decision, req.Comment = strings.TrimSpace(req.Decision), strings.TrimSpace(req.Comment)
	if req.Decision != "approve" && req.Decision != "reject" {
		middleware.WriteError(w, 400, "выберите подтверждение или отклонение")
		return
	}
	if req.Decision == "reject" && utf8.RuneCountInString(req.Comment) < 5 {
		middleware.WriteError(w, 400, "укажите причину отклонения")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var currentStatus string
	if err = tx.QueryRowContext(r.Context(), `SELECT verification_status FROM education_directory
		WHERE id::text=$1 AND proposed_by IS NOT NULL FOR UPDATE`, id).Scan(&currentStatus); err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "предложение не найдено")
		return
	} else if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения предложения")
		return
	}
	if currentStatus != "pending" {
		middleware.WriteError(w, 409, "по предложению уже принято решение")
		return
	}
	if req.Decision == "approve" {
		var write directoryWriteRequest
		var codes pq.StringArray
		err = tx.QueryRowContext(r.Context(), `SELECT name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
			COALESCE(license_number,''),license_status,institution_status,COALESCE(source_url,''),
			COALESCE(registry_updated_at::text,''),program_codes,programs_source_url
			FROM education_directory WHERE id::text=$1`, id).Scan(&write.Name, &write.PartnerKind, &write.Region,
			&write.INN, &write.OGRN, &write.LicenseNumber, &write.LicenseStatus, &write.InstitutionStatus,
			&write.SourceURL, &write.RegistryUpdatedAt, &codes, &write.ProgramsSourceURL)
		normalizedCodes := []string(codes)
		write.ProgramCodes, write.Confirm = &normalizedCodes, true
		if err == nil {
			err = normalizeDirectoryWrite(&write)
		}
		if err != nil || write.LicenseStatus != "active" || write.InstitutionStatus != "active" {
			middleware.WriteError(w, 409, "сначала откройте предложение и заполните актуальные реквизиты, действующую лицензию и статус организации")
			return
		}
		var matchesOrder bool
		if err = tx.QueryRowContext(r.Context(), `SELECT education_matches_order($1,$2)`, write.PartnerKind, pq.Array(normalizedCodes)).Scan(&matchesOrder); err != nil {
			middleware.WriteError(w, 500, "не удалось проверить направления подготовки")
			return
		}
		if !matchesOrder {
			middleware.WriteError(w, 409, "в предложении нет направления из перечня Минцифры")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE education_directory SET verification_status='verified',
			verified_by=$1,verified_at=now(),reviewed_by=$1,reviewed_at=now(),review_comment=$2,updated_at=now(),
			listed_in_mincifry_order_27=listed_in_mincifry_order_27 OR
				(partner_kind='vuz' AND source_url=$4)
			WHERE id::text=$3`, u.ID, req.Comment, id, ministryEducationDirectoryURL)
	} else {
		_, err = tx.ExecContext(r.Context(), `UPDATE education_directory SET verification_status='rejected',
			verified_by=NULL,verified_at=NULL,reviewed_by=$1,reviewed_at=now(),review_comment=$2,updated_at=now()
			WHERE id::text=$3`, u.ID, req.Comment, id)
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сохранить решение")
		return
	}
	if err = logAudit(r.Context(), tx, "education_directory", id, "directory_proposal_"+req.Decision, u.ID, req.Comment,
		map[string]interface{}{"verification_status": "pending"}, map[string]interface{}{"verification_status": map[string]string{"approve": "verified", "reject": "rejected"}[req.Decision]}); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "не удалось сохранить решение")
		return
	}
	middleware.WriteJSON(w, 200, map[string]string{"id": id, "decision": req.Decision})
}
