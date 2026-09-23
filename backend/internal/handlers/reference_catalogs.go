package handlers

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"cybercalc/internal/xlsx"
)

type ReferenceCatalogHandlers struct{ DB *sql.DB }

func catalogAdmin(u middleware.AuthUser) bool {
	return u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin
}

func (h *ReferenceCatalogHandlers) Organizations(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	rows, err := h.DB.QueryContext(r.Context(), `SELECT source_type,id::text,name,organization_kind,inn,kpp,ogrn,
		legal_address,accreditation_number,accreditation_status,organization_role,source_url,updated_at
		FROM organization_directory_v44 WHERE ($1='' OR organization_kind=$1)
		AND ($2='' OR name ILIKE '%'||$2||'%' OR inn ILIKE '%'||$2||'%' OR ogrn ILIKE '%'||$2||'%')
		ORDER BY name,source_type,id`+page, kind, q)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения каталога организаций")
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var sourceType, id, name, organizationKind, inn, kpp, ogrn, address, accreditationNumber, accreditationStatus, role, sourceURL string
		var updated time.Time
		if err := rows.Scan(&sourceType, &id, &name, &organizationKind, &inn, &kpp, &ogrn, &address,
			&accreditationNumber, &accreditationStatus, &role, &sourceURL, &updated); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения каталога организаций")
			return
		}
		items = append(items, map[string]interface{}{"source_type": sourceType, "id": id, "name": name,
			"organization_kind": organizationKind, "inn": inn, "kpp": kpp, "ogrn": ogrn,
			"legal_address": address, "accreditation_number": accreditationNumber,
			"accreditation_status": accreditationStatus, "organization_role": role,
			"source_url": sourceURL, "updated_at": updated})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения каталога организаций")
		return
	}
	writePage(w, r, items)
}

func (h *ReferenceCatalogHandlers) Specialties(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
	page, ok := pageClause(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	system := strings.TrimSpace(r.URL.Query().Get("education_system"))
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id::text,code,education_system,qualification_level,title,
		catalog_version,effective_from,source_reference,COALESCE(normative_source_id::text,'')
		FROM active_specialties WHERE ($1='' OR education_system=$1)
		AND ($2='' OR code ILIKE '%'||$2||'%' OR title ILIKE '%'||$2||'%')
		ORDER BY code`+page, system, q)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка чтения специальностей")
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, code, educationSystem, level, title, version, source, sourceID string
		var effective time.Time
		if err := rows.Scan(&id, &code, &educationSystem, &level, &title, &version, &effective, &source, &sourceID); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения специальностей")
			return
		}
		items = append(items, map[string]interface{}{"id": id, "code": code, "education_system": educationSystem,
			"qualification_level": level, "title": title, "catalog_version": version,
			"effective_from": effective.Format("2006-01-02"), "source_reference": source, "normative_source_id": sourceID})
	}
	writePage(w, r, items)
}

var catalogSpecialtyCodePattern = regexp.MustCompile(`^[0-9]{2}\.[0-9]{2}\.[0-9]{2}$`)

func multipartTable(w http.ResponseWriter, r *http.Request) ([][]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		return nil, fmt.Errorf("некорректный multipart или файл больше 4 МБ")
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("поле file обязательно")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		return nil, fmt.Errorf("ожидается CSV или XLSX до 4 МБ")
	}
	if strings.EqualFold(filepath.Ext(header.Filename), ".xlsx") {
		records, err := xlsx.ReadFirst(body)
		if err != nil || len(records) < 2 || len(records) > 10001 {
			return nil, fmt.Errorf("XLSX повреждён или содержит недопустимое число строк")
		}
		return records, nil
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("CSV должен быть в UTF-8")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(body), "\ufeff")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) < 2 || len(records) > 10001 {
		return nil, fmt.Errorf("CSV повреждён или содержит недопустимое число строк")
	}
	return records, nil
}

func headerPositions(header []string, required ...string) (map[string]int, error) {
	positions := map[string]int{}
	for index, value := range header {
		value = strings.TrimSpace(value)
		if _, exists := positions[value]; value == "" || exists {
			return nil, fmt.Errorf("пустой или повторный заголовок CSV")
		}
		positions[value] = index
	}
	for _, name := range required {
		if _, exists := positions[name]; !exists {
			return nil, fmt.Errorf("нет обязательного столбца %s", name)
		}
	}
	return positions, nil
}

func cell(row []string, positions map[string]int, name string) string {
	index, ok := positions[name]
	if !ok || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func (h *ReferenceCatalogHandlers) ImportSpecialties(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !catalogAdmin(u) {
		middleware.WriteError(w, 403, "импорт справочника доступен системному администратору")
		return
	}
	records, err := multipartTable(w, r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	positions, err := headerPositions(records[0], "code", "education_system", "title")
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	versionCode := strings.TrimSpace(r.FormValue("version_code"))
	versionTitle := strings.TrimSpace(r.FormValue("title"))
	effectiveFrom, err := time.Parse("2006-01-02", r.FormValue("effective_from"))
	if err != nil || versionCode == "" || versionTitle == "" {
		middleware.WriteError(w, 400, "укажите version_code, title и effective_from")
		return
	}
	sourceID := strings.TrimSpace(r.FormValue("normative_source_id"))
	if sourceID == "" {
		middleware.WriteError(w, 400, "normative_source_id обязателен")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var sourceReference string
	if err := tx.QueryRowContext(r.Context(), `SELECT title||', редакция '||revision||', SHA-256 '||content_sha256 FROM normative_sources WHERE id::text=$1`, sourceID).Scan(&sourceReference); err != nil {
		middleware.WriteError(w, 400, "нормативный источник не найден")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE specialty_catalog_versions SET status='retired',effective_until=CASE WHEN effective_from<$1 THEN $1::date-1 ELSE effective_from END WHERE status='active'`, effectiveFrom); err != nil {
		middleware.WriteError(w, 500, "ошибка переключения версии")
		return
	}
	var versionID string
	if err := tx.QueryRowContext(r.Context(), `INSERT INTO specialty_catalog_versions(code,title,effective_from,normative_source_id,source_reference,status,created_by)
		VALUES($1,$2,$3,$4,$5,'active',$6) RETURNING id::text`, versionCode, versionTitle, effectiveFrom, sourceID, sourceReference, u.ID).Scan(&versionID); err != nil {
		middleware.WriteError(w, 409, "версия справочника конфликтует с существующей")
		return
	}
	seen := map[string]bool{}
	for index, row := range records[1:] {
		code := cell(row, positions, "code")
		system := cell(row, positions, "education_system")
		title := cell(row, positions, "title")
		if !catalogSpecialtyCodePattern.MatchString(code) || (system != "higher" && system != "secondary_vocational") || title == "" || seen[code] {
			middleware.WriteError(w, 400, fmt.Sprintf("некорректная или повторная специальность в строке %d", index+2))
			return
		}
		seen[code] = true
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO specialties(catalog_version_id,code,education_system,qualification_level,title)
			VALUES($1,$2,$3,$4,$5)`, versionID, code, system, cell(row, positions, "qualification_level"), title); err != nil {
			middleware.WriteError(w, 400, fmt.Sprintf("не удалось сохранить строку %d", index+2))
			return
		}
	}
	if err := logAudit(r.Context(), tx, "specialty_catalog", versionID, "import", u.ID, "", nil, map[string]interface{}{"code": versionCode, "rows": len(seen), "source_id": sourceID}); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения справочника")
		return
	}
	middleware.WriteJSON(w, 201, map[string]interface{}{"id": versionID, "rows": len(seen)})
}

func (h *ReferenceCatalogHandlers) Tariffs(w http.ResponseWriter, r *http.Request, _ middleware.AuthUser) {
	year, err := strconv.Atoi(r.URL.Query().Get("report_year"))
	activity := strings.TrimSpace(r.URL.Query().Get("activity_code"))
	audience := strings.TrimSpace(r.URL.Query().Get("audience"))
	if err != nil || activity == "" || audience == "" {
		middleware.WriteError(w, 400, "укажите report_year, activity_code и audience")
		return
	}
	version, err := (tariffs.Repository{DB: h.DB}).Active(r.Context(), year, activity, audience)
	if err != nil {
		if errors.Is(err, tariffs.ErrNoActiveVersion) {
			middleware.WriteError(w, 404, err.Error())
		} else {
			middleware.WriteError(w, 500, "ошибка чтения тарифов")
		}
		return
	}
	middleware.WriteJSON(w, 200, version)
}

func (h *ReferenceCatalogHandlers) ImportTariffs(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !catalogAdmin(u) {
		middleware.WriteError(w, 403, "импорт тарифов доступен системному администратору")
		return
	}
	records, err := multipartTable(w, r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	positions, err := headerPositions(records[0], "activity_code", "audience", "component_code", "rate_rub", "unit_code")
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	versionCode := strings.TrimSpace(r.FormValue("version_code"))
	versionTitle := strings.TrimSpace(r.FormValue("title"))
	effectiveFrom, err := time.Parse("2006-01-02", r.FormValue("effective_from"))
	if err != nil || versionCode == "" || versionTitle == "" {
		middleware.WriteError(w, 400, "укажите version_code, title и effective_from")
		return
	}
	sourceID := strings.TrimSpace(r.FormValue("normative_source_id"))
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	var sourceReference string
	if sourceID == "" || tx.QueryRowContext(r.Context(), `SELECT title||', редакция '||revision||', SHA-256 '||content_sha256 FROM normative_sources WHERE id::text=$1`, sourceID).Scan(&sourceReference) != nil {
		middleware.WriteError(w, 400, "выберите проверенный normative_source_id")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tariff_versions SET status='retired',effective_until=CASE WHEN effective_from<$1 THEN $1::date-1 ELSE effective_from END WHERE status='active'`, effectiveFrom); err != nil {
		middleware.WriteError(w, 500, "ошибка переключения тарифов")
		return
	}
	var versionID string
	if err := tx.QueryRowContext(r.Context(), `INSERT INTO tariff_versions(code,title,effective_from,normative_source_id,source_reference,provenance_verified,status,created_by)
		VALUES($1,$2,$3,$4,$5,TRUE,'active',$6) RETURNING id::text`, versionCode, versionTitle, effectiveFrom, sourceID, sourceReference, u.ID).Scan(&versionID); err != nil {
		middleware.WriteError(w, 409, "версия тарифов конфликтует с существующей")
		return
	}
	seen := map[string]bool{}
	for index, row := range records[1:] {
		activity := cell(row, positions, "activity_code")
		audience := cell(row, positions, "audience")
		component := cell(row, positions, "component_code")
		rate := strings.ReplaceAll(cell(row, positions, "rate_rub"), ",", ".")
		unit := cell(row, positions, "unit_code")
		key := activity + "\x00" + audience + "\x00" + component
		if _, err := money.Parse(rate); err != nil || activity == "" || component == "" || unit == "" || seen[key] ||
			(audience != "vuz" && audience != "kolledj" && audience != "school" && audience != "all") {
			middleware.WriteError(w, 400, fmt.Sprintf("некорректная или повторная тарифная строка %d", index+2))
			return
		}
		seen[key] = true
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO tariff_rules(tariff_version_id,activity_code,audience,component_code,rate_rub,unit_code)
			VALUES($1,$2,$3,$4,$5,$6)`, versionID, activity, audience, component, rate, unit); err != nil {
			middleware.WriteError(w, 400, fmt.Sprintf("не удалось сохранить тарифную строку %d", index+2))
			return
		}
	}
	if err := logAudit(r.Context(), tx, "tariff_version", versionID, "import", u.ID, "", nil, map[string]interface{}{"code": versionCode, "rows": len(seen), "source_id": sourceID}); err != nil || tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения тарифов")
		return
	}
	middleware.WriteJSON(w, 201, map[string]interface{}{"id": versionID, "rows": len(seen)})
}
