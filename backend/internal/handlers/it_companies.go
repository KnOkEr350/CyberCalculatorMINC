package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/mail"
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
	LegalAddress        string `json:"legal_address"`
	Phone               string `json:"phone"`
	Email               string `json:"email"`
	Website             string `json:"website"`
	DirectorName        string `json:"director_name"`
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
	req.LegalAddress = strings.Join(strings.Fields(req.LegalAddress), " ")
	req.Phone = strings.TrimSpace(req.Phone)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Website = strings.TrimSpace(req.Website)
	req.DirectorName = strings.Join(strings.Fields(req.DirectorName), " ")
	_, dateErr := time.Parse("2006-01-02", req.RegistryUpdatedAt)
	// Regulatory dates are Moscow calendar dates. Comparing UTC midnights made a
	// valid date look like "tomorrow" during the first three hours of the day.
	moscowToday := time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01-02")
	if utf8.RuneCountInString(req.Name) < 2 || utf8.RuneCountInString(req.Name) > 1000 ||
		!validINN(req.INN) || !validOGRN(req.OGRN) ||
		utf8.RuneCountInString(req.AccreditationNumber) > 100 ||
		req.RegistryRecordID == "" || utf8.RuneCountInString(req.RegistryRecordID) > 200 ||
		dateErr != nil || req.RegistryUpdatedAt > moscowToday || !officialITRegistryURL(req.SourceURL) ||
		utf8.RuneCountInString(req.Notes) > 1000 || utf8.RuneCountInString(req.LegalAddress) > 1000 ||
		utf8.RuneCountInString(req.Phone) > 100 || utf8.RuneCountInString(req.Email) > 254 ||
		utf8.RuneCountInString(req.Website) > 1000 || utf8.RuneCountInString(req.DirectorName) > 300 {
		return req, fmt.Errorf("проверьте название, ИНН/ОГРН, дату проверки и официальную HTTPS-ссылку на реестр")
	}
	if req.Email != "" {
		address, err := mail.ParseAddress(req.Email)
		if err != nil || address.Address != req.Email {
			return req, fmt.Errorf("укажите корректный email ИТ-компании")
		}
	}
	if req.Website != "" {
		website, err := url.Parse(req.Website)
		if err != nil || website.Scheme != "https" || website.Hostname() == "" || website.User != nil {
			return req, fmt.Errorf("сайт ИТ-компании должен быть корректной HTTPS-ссылкой")
		}
	}
	return req, nil
}

func (h *ITCompanyHandlers) List(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canViewITCompanies(u) {
		middleware.WriteError(w, http.StatusForbidden, "реестр ИТ-компаний недоступен для этого профиля")
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
		accreditation_status,registry_record_id,registry_updated_at::text,source_url,notes,
		legal_address,phone,email,website,director_name,created_at
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
			&company.RegistryUpdatedAt, &company.SourceURL, &company.Notes, &company.LegalAddress,
			&company.Phone, &company.Email, &company.Website, &company.DirectorName, &company.CreatedAt); err != nil {
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
		middleware.WriteError(w, http.StatusForbidden, "добавлять ИТ-компании нельзя из этого профиля")
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
		name,inn,ogrn,accreditation_number,registry_record_id,registry_updated_at,source_url,notes,
		legal_address,phone,email,website,director_name,created_by)
		VALUES($1,$2,$3,$4,$5,$6::date,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, req.Name, req.INN, req.OGRN,
		req.AccreditationNumber, req.RegistryRecordID, req.RegistryUpdatedAt, req.SourceURL, req.Notes,
		req.LegalAddress, req.Phone, req.Email, req.Website, req.DirectorName, u.ID).Scan(&id)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, "компания с такими реквизитами или записью реестра уже существует")
		return
	}
	if logAudit(r.Context(), tx, "it_company", id, "create", u.ID, "добавлена из официального реестра аккредитованных ИТ-компаний", nil, req) != nil || tx.Commit() != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось сохранить ИТ-компанию")
		return
	}
	middleware.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}
