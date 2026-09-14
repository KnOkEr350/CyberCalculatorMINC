package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"cybercalc/internal/xlsx"
)

var itCompanyColumns = []string{"name", "inn", "ogrn", "accreditation_number", "accreditation_status", "registry_record_id", "registry_updated_at", "source_url", "notes"}
var itCompanyLabels = []string{"Наименование", "ИНН", "ОГРН", "Номер аккредитации", "Статус аккредитации", "Идентификатор записи реестра", "Дата актуальности сведений", "Ссылка на официальный источник", "Примечание"}

type importedITCompany struct {
	itCompanyWriteRequest
	Status string
}

// ReadITCompanies accepts full UTF-8 CSV or XLSX snapshots; it does not scrape
// a search-engine sample or silently truncate at the first page.
func ReadITCompanies(data []byte) ([]importedITCompany, error) {
	if len(data) > 32<<20 {
		return nil, fmt.Errorf("выгрузка превышает 32 МБ")
	}
	var rows [][]string
	var err error
	if bytes.HasPrefix(data, []byte("PK")) {
		rows, err = xlsx.ReadFirst(data)
	} else {
		data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("сохраните CSV в кодировке UTF-8")
		}
		reader := csv.NewReader(bytes.NewReader(data))
		header := strings.SplitN(string(data), "\n", 2)[0]
		if strings.Count(header, ";") > strings.Count(header, ",") {
			reader.Comma = ';'
		}
		reader.FieldsPerRecord = -1
		rows, err = reader.ReadAll()
	}
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 || len(rows) > 100001 {
		return nil, fmt.Errorf("нужны заголовки и от 1 до 100000 компаний")
	}
	columns := map[string]int{}
	for index, raw := range rows[0] {
		header := strings.TrimSpace(raw)
		for j, key := range itCompanyColumns {
			if strings.EqualFold(header, key) || strings.EqualFold(header, itCompanyLabels[j]) {
				if _, exists := columns[key]; exists {
					return nil, fmt.Errorf("повторный столбец %s", header)
				}
				columns[key] = index
			}
		}
	}
	for _, key := range []string{"name", "inn", "ogrn", "registry_updated_at", "source_url"} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("нет обязательного столбца %s", key)
		}
	}
	result := []importedITCompany{}
	seen := map[string]importedITCompany{}
	ogrns, records := map[string]string{}, map[string]string{}
	for index, row := range rows[1:] {
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		if len(row) > len(rows[0]) {
			return nil, fmt.Errorf("строка %d: лишние ячейки", index+2)
		}
		get := func(key string) string {
			col, ok := columns[key]
			if ok && col < len(row) {
				return strings.TrimSpace(row[col])
			}
			return ""
		}
		req, err := normalizeITCompany(itCompanyWriteRequest{Name: get("name"), INN: get("inn"), OGRN: get("ogrn"), AccreditationNumber: get("accreditation_number"), RegistryRecordID: get("registry_record_id"), RegistryUpdatedAt: get("registry_updated_at"), SourceURL: get("source_url"), Notes: get("notes")})
		if err != nil {
			return nil, fmt.Errorf("строка %d: %w", index+2, err)
		}
		status := strings.ToLower(get("accreditation_status"))
		switch status {
		case "", "действует":
			status = "active"
		case "приостановлена":
			status = "suspended"
		case "аннулирована", "исключена":
			status = "revoked"
		}
		if !oneOf(status, "active", "suspended", "revoked") {
			return nil, fmt.Errorf("строка %d: неизвестный статус аккредитации", index+2)
		}
		date, _ := time.Parse("2006-01-02", req.RegistryUpdatedAt)
		if status == "active" && !freshRegistryDate(date, time.Now()) {
			return nil, fmt.Errorf("строка %d: сведения о действующей аккредитации старше 35 дней", index+2)
		}
		item := importedITCompany{req, status}
		if prior, ok := seen[req.INN]; ok {
			if prior == item {
				continue
			}
			return nil, fmt.Errorf("строка %d: противоречивые записи ИНН %s", index+2, req.INN)
		}
		if (ogrns[req.OGRN] != "" && ogrns[req.OGRN] != req.INN) || (records[req.RegistryRecordID] != "" && records[req.RegistryRecordID] != req.INN) {
			return nil, fmt.Errorf("строка %d: конфликт ОГРН или записи реестра", index+2)
		}
		seen[req.INN], ogrns[req.OGRN], records[req.RegistryRecordID] = item, req.INN, req.INN
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("выгрузка не содержит компаний")
	}
	return result, nil
}

func ImportITCompanies(ctx context.Context, db *sql.DB, data []byte, userID string) (int, error) {
	companies, err := ReadITCompanies(data)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1129989447)`); err != nil {
		return 0, err
	}
	if userID == "" {
		if err = tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE role='admin' ORDER BY created_at,id LIMIT 1`).Scan(&userID); err != nil {
			return 0, err
		}
	}
	for _, company := range companies {
		var id string
		// OGRN mismatch is an identity conflict, not permission to overwrite
		// another legal entity. New snapshots also cannot roll back newer data.
		err = tx.QueryRowContext(ctx, `INSERT INTO accredited_it_companies(name,inn,ogrn,accreditation_number,accreditation_status,registry_record_id,registry_updated_at,source_url,notes,created_by)
		 VALUES($1,$2,$3,$4,$5,$6,$7::date,$8,$9,$10)
		 ON CONFLICT(inn) DO UPDATE SET name=EXCLUDED.name,accreditation_number=EXCLUDED.accreditation_number,
		 accreditation_status=EXCLUDED.accreditation_status,registry_record_id=EXCLUDED.registry_record_id,
		 registry_updated_at=EXCLUDED.registry_updated_at,source_url=EXCLUDED.source_url,notes=EXCLUDED.notes,updated_at=now()
		 WHERE accredited_it_companies.ogrn=EXCLUDED.ogrn AND accredited_it_companies.registry_updated_at<=EXCLUDED.registry_updated_at
		 RETURNING id::text`, company.Name, company.INN, company.OGRN, company.AccreditationNumber, company.Status, company.RegistryRecordID, company.RegistryUpdatedAt, company.SourceURL, company.Notes, userID).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("импорт отменён: конфликт или более новые сведения для ИНН %s: %w", company.INN, err)
		}
	}
	if err = logAudit(tx, "it_company", "", "import", userID, fmt.Sprintf("Загружено %d компаний из выгрузки реестра", len(companies)), nil, map[string]int{"rows": len(companies)}); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(companies), nil
}

func (h *ITCompanyHandlers) Import(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageITCompanies(u) {
		middleware.WriteError(w, 403, "нет доступа к реестру ИТ-компаний")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 33<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		middleware.WriteError(w, 400, "нужна выгрузка CSV или XLSX до 32 МБ")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		middleware.WriteError(w, 400, "выберите файл выгрузки")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		middleware.WriteError(w, 400, "не удалось прочитать файл")
		return
	}
	companies, err := ReadITCompanies(data)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	committed := r.URL.Query().Get("commit") == "1"
	if committed {
		if _, err = ImportITCompanies(r.Context(), h.DB, data, u.ID); err != nil {
			middleware.WriteError(w, 409, err.Error())
			return
		}
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"valid_rows": len(companies), "committed": committed})
}

func (h *ITCompanyHandlers) Template(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageITCompanies(u) {
		middleware.WriteError(w, 403, "нет доступа к реестру ИТ-компаний")
		return
	}
	wb := xlsx.New()
	wb.AddSheet("ИТ-компании", itCompanyLabels, nil)
	writeWorkbook(w, wb, "шаблон_ИТ_компаний.xlsx")
}
