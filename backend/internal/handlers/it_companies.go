package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

type ITCompanyHandlers struct {
	DB *sql.DB
}

type itCompanyWriteRequest struct {
	Name                string `json:"name"`
	INN                 string `json:"inn"`
	OGRN                string `json:"ogrn"`
	AccreditationNumber string `json:"accreditation_number"`
	RegistryRecordID    string `json:"registry_record_id"`
	RegistryUpdatedAt   string `json:"registry_updated_at"`
	SourceURL           string `json:"source_url"`
	Notes               string `json:"notes"`
}

func officialITRegistryURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "gosuslugi.ru" || strings.HasSuffix(host, ".gosuslugi.ru") ||
		host == "digital.gov.ru" || strings.HasSuffix(host, ".digital.gov.ru") ||
		strings.HasSuffix(host, ".gov.ru")
}

func normalizeITCompany(req itCompanyWriteRequest) (itCompanyWriteRequest, error) {
	req.Name = strings.Join(strings.Fields(req.Name), " ")
	req.INN = strings.TrimSpace(req.INN)
	req.OGRN = strings.TrimSpace(req.OGRN)
	req.AccreditationNumber = strings.TrimSpace(req.AccreditationNumber)
	req.RegistryRecordID = strings.TrimSpace(req.RegistryRecordID)
	if req.RegistryRecordID == "" {
		req.RegistryRecordID = req.INN
	}
	req.RegistryUpdatedAt = strings.TrimSpace(req.RegistryUpdatedAt)
	req.SourceURL = strings.TrimSpace(req.SourceURL)
	req.Notes = strings.TrimSpace(req.Notes)
	registryDate, dateErr := time.Parse("2006-01-02", req.RegistryUpdatedAt)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if utf8.RuneCountInString(req.Name) < 2 || utf8.RuneCountInString(req.Name) > 1000 ||
		!validINN(req.INN) || !validOGRN(req.OGRN) ||
		utf8.RuneCountInString(req.AccreditationNumber) > 100 ||
		req.RegistryRecordID == "" || utf8.RuneCountInString(req.RegistryRecordID) > 200 ||
		dateErr != nil || registryDate.After(today) || !officialITRegistryURL(req.SourceURL) ||
		utf8.RuneCountInString(req.Notes) > 1000 {
		return req, fmt.Errorf("проверьте название, ИНН/ОГРН, дату проверки и официальную HTTPS-ссылку на реестр")
	}
	return req, nil
}

func (h *ITCompanyHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageITCompanies(u) {
		middleware.WriteError(w, http.StatusForbidden, "реестр доступен представителям ИТ-организаций")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 200 {
		middleware.WriteError(w, http.StatusBadRequest, "поисковый запрос не должен превышать 200 символов")
		return
	}
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,name,inn,ogrn,accreditation_number,
		accreditation_status,registry_record_id,registry_updated_at::text,source_url,notes,created_at
		FROM accredited_it_companies
		WHERE accreditation_status='active'
		AND ($1='' OR name ILIKE '%'||$1||'%' OR inn=$1 OR ogrn=$1 OR
			accreditation_number ILIKE '%'||$1||'%' OR registry_record_id ILIKE '%'||$1||'%')
		ORDER BY name,id`+page, q)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось загрузить реестр ИТ-компаний")
		return
	}
	defer rows.Close()
	out := []models.ITCompany{}
	for rows.Next() {
		var company models.ITCompany
		if err := rows.Scan(&company.ID, &company.Name, &company.INN, &company.OGRN,
			&company.AccreditationNumber, &company.AccreditationStatus, &company.RegistryRecordID,
			&company.RegistryUpdatedAt, &company.SourceURL, &company.Notes, &company.CreatedAt); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "не удалось прочитать реестр ИТ-компаний")
			return
		}
		out = append(out, company)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось прочитать реестр ИТ-компаний")
		return
	}
	writePage(w, r, out)
}

func (h *ITCompanyHandlers) Create(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageITCompanies(u) {
		middleware.WriteError(w, http.StatusForbidden, "добавлять ИТ-компании могут представители ИТ-организаций")
		return
	}
	var req itCompanyWriteRequest
	if decodeJSON(r, &req) != nil {
		middleware.WriteError(w, http.StatusBadRequest, "некорректный запрос")
		return
	}
	var err error
	if req, err = normalizeITCompany(req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(r.Context(), `INSERT INTO accredited_it_companies(
		name,inn,ogrn,accreditation_number,registry_record_id,registry_updated_at,source_url,notes,created_by)
		VALUES($1,$2,$3,$4,$5,$6::date,$7,$8,$9) RETURNING id`, req.Name, req.INN, req.OGRN,
		req.AccreditationNumber, req.RegistryRecordID, req.RegistryUpdatedAt, req.SourceURL, req.Notes, u.ID).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "компания с такими реквизитами или записью реестра уже существует")
		return
	}
	if logAudit(tx, "it_company", id, "create", u.ID, "добавлена из официального реестра аккредитованных ИТ-компаний", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось сохранить ИТ-компанию")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}
