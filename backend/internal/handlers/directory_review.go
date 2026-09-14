package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
)

type directoryWriteRequest struct {
	Name                string `json:"name"`
	PartnerKind         string `json:"partner_kind"`
	Region              string `json:"region"`
	INN                 string `json:"inn"`
	OGRN                string `json:"ogrn"`
	LicenseNumber       string `json:"license_number"`
	LicenseStatus       string `json:"license_status"`
	InstitutionStatus   string `json:"institution_status"`
	SourceURL           string `json:"source_url"`
	RegistryUpdatedAt   string `json:"registry_updated_at"`
	Confirm             bool   `json:"confirm"`
	ConfirmationComment string `json:"confirmation_comment"`
}

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
		if err != nil || date.After(time.Now().UTC().Truncate(24*time.Hour)) {
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
	if !canReviewEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к проверке учебных заведений")
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
	if _, err = tx.ExecContext(r.Context(), `UPDATE partners SET name=$1,partner_kind=$2 WHERE directory_id::text=$3`, req.Name, req.PartnerKind, id); err != nil {
		middleware.WriteError(w, 500, "не удалось обновить связанного партнёра")
		return
	}
	newValue := directoryAuditValue(req.Name, req.PartnerKind, req.Region, req.INN, req.OGRN,
		req.LicenseNumber, req.LicenseStatus, req.InstitutionStatus, recordID, req.SourceURL,
		req.RegistryUpdatedAt, status)
	action := "directory_update"
	comment := "Реквизиты изменены; требуется подтверждение"
	if req.Confirm {
		action = "directory_confirm"
		comment = "Учебное заведение подтверждено"
	}
	if req.ConfirmationComment != "" {
		comment += ": " + req.ConfirmationComment
	}
	if err = logAudit(tx, "education_directory", id, action, u.ID, comment, oldValue, newValue); err != nil {
		middleware.WriteError(w, 500, "ошибка аудита")
		return
	}
	if err = tx.Commit(); err != nil {
		middleware.WriteError(w, 500, "ошибка сохранения")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{"id": id, "verification_status": status})
}
