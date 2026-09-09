package handlers

import (
	"crypto/sha256"
	"cybercalc/internal/calculators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/xlsx"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func importFields(category string) ([]calculators.FieldSpec, error) {
	c, err := calculators.Get(category)
	if err != nil {
		return nil, err
	}
	out := []calculators.FieldSpec{}
	for _, f := range c.Fields() {
		if f.Key != "org_name" && f.Key != "mentor_id" {
			out = append(out, f)
		}
	}
	return out, nil
}
func (h *EntryHandlers) ImportTemplate(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	fields, err := importFields(r.URL.Query().Get("category_code"))
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	headers := []string{}
	info := [][]interface{}{}
	for _, f := range fields {
		headers = append(headers, f.Key)
		info = append(info, []interface{}{f.Key, f.Label, f.Required, strings.Join(f.Options, ", ")})
	}
	wb := xlsx.New()
	wb.AddSheet("Данные", headers, nil)
	wb.AddSheet("Инструкция", []string{"Поле", "Название", "Обязательно", "Допустимые значения"}, info)
	writeWorkbook(w, wb, "import_template.xlsx")
}
func writeWorkbook(w http.ResponseWriter, wb *xlsx.Workbook, name string) {
	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка создания Excel")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Write(body)
}
func uploadedWorkbook(w http.ResponseWriter, r *http.Request) ([]byte, [][]string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			r.MultipartForm.RemoveAll()
		}
		return nil, nil, fmt.Errorf("выберите .xlsx до 8 МБ")
	}
	defer r.MultipartForm.RemoveAll()
	f, header, err := r.FormFile("file")
	if err != nil {
		return nil, nil, fmt.Errorf("поле file обязательно")
	}
	defer f.Close()
	if header.Size > 8<<20 {
		return nil, nil, fmt.Errorf("файл превышает 8 МБ")
	}
	data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil {
		return nil, nil, err
	}
	rows, err := xlsx.ReadFirst(data)
	return data, rows, err
}

type importRow struct {
	Row     int                    `json:"row"`
	Payload map[string]interface{} `json:"payload"`
	Amount  money.Amount           `json:"amount_rub"`
}
type importResult struct {
	Rows      []importRow  `json:"rows"`
	Errors    []string     `json:"errors"`
	Total     money.Amount `json:"total_rub"`
	Committed bool         `json:"committed"`
}

func (h *EntryHandlers) Import(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	category, partner, period := q.Get("category_code"), q.Get("partner_id"), q.Get("period_type")
	year, err := strconv.Atoi(q.Get("report_year"))
	if err != nil || year < 2000 || year > 2100 || (period != "plan" && period != "fact") {
		middleware.WriteError(w, 400, "укажите корректные год и план/факт")
		return
	}
	if !requirePartner(w, u, partner) {
		return
	}
	var audience string
	if h.DB.QueryRowContext(r.Context(), `SELECT partner_kind FROM partners WHERE id::text=$1`, partner).Scan(&audience) != nil {
		middleware.WriteError(w, 400, "партнёр не найден")
		return
	}
	if err := h.validateEntryContext(r, category, audience, partner); err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	data, table, err := uploadedWorkbook(w, r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if len(table) > 1001 {
		middleware.WriteError(w, 400, "не более 1000 строк за один импорт")
		return
	}
	fields, _ := importFields(category)
	specs := map[string]calculators.FieldSpec{}
	labels := map[string]string{}
	for _, f := range fields {
		specs[f.Key] = f
		labels[f.Label] = f.Key
	}
	keys := []string{}
	seen := map[string]bool{}
	for _, header := range table[0] {
		key := header
		if mapped, ok := labels[header]; ok {
			key = mapped
		}
		if _, ok := specs[key]; !ok || seen[key] {
			middleware.WriteError(w, 400, "неизвестный или повторный столбец: "+header)
			return
		}
		seen[key] = true
		keys = append(keys, key)
	}
	for _, f := range fields {
		if f.Required && !seen[f.Key] {
			middleware.WriteError(w, 400, "нет обязательного столбца: "+f.Key)
			return
		}
	}
	result := importResult{Rows: []importRow{}, Errors: []string{}}
	calc, _ := calculators.Get(category)
	for i, values := range table[1:] {
		if strings.TrimSpace(strings.Join(values, "")) == "" {
			continue
		}
		row := importRow{Row: i + 2, Payload: map[string]interface{}{"org_name": partner}}
		rowErr := func() error {
			if len(values) > len(keys) {
				return fmt.Errorf("есть данные за пределами заголовков")
			}
			for j, key := range keys {
				value := ""
				if j < len(values) {
					value = values[j]
				}
				f := specs[key]
				if value == "" && !f.Required {
					continue
				}
				if f.Type == "number" {
					n := strings.ReplaceAll(value, ",", ".")
					_, e := strconv.ParseFloat(n, 64)
					if e != nil {
						return fmt.Errorf("%s: нужно число", f.Label)
					}
					row.Payload[key] = json.Number(n)
				} else {
					row.Payload[key] = value
				}
			}
			if category == "internship" || category == "employment_practice" {
				var id string
				name, _ := row.Payload["mentor_full_name"].(string)
				if h.DB.QueryRowContext(r.Context(), `SELECT id FROM mentors WHERE partner_id::text=$1 AND lower(full_name)=lower($2)`, partner, strings.Join(strings.Fields(name), " ")).Scan(&id) != nil {
					return fmt.Errorf("наставник %q отсутствует в справочнике партнёра", name)
				}
				row.Payload["mentor_id"] = id
			}
			if err := h.validateMentor(r, category, partner, row.Payload); err != nil {
				return err
			}
			if err := calculators.ValidatePayload(calc, row.Payload); err != nil {
				return err
			}
			amount, e := calculators.CalculateAmount(category, models.Audience(audience), row.Payload)
			if e != nil {
				return e
			}
			if e := calculators.ValidateAmount(amount.Rubles()); e != nil {
				return e
			}
			row.Amount = amount
			return nil
		}()
		if rowErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Строка %d: %s", row.Row, rowErr))
		} else {
			result.Rows = append(result.Rows, row)
			var sumErr error
			result.Total, sumErr = money.Add(result.Total, row.Amount)
			if sumErr != nil {
				middleware.WriteError(w, 400, sumErr.Error())
				return
			}
		}
	}
	if len(result.Rows) == 0 && len(result.Errors) == 0 {
		result.Errors = append(result.Errors, "нет строк данных")
	}
	if q.Get("commit") != "1" || len(result.Errors) > 0 {
		middleware.WriteJSON(w, 200, result)
		return
	}
	context, _ := json.Marshal([]interface{}{u.ID, category, partner, period, year})
	digest := sha256.Sum256(append(context, data...))
	fingerprint := hex.EncodeToString(digest[:])
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка транзакции")
		return
	}
	defer tx.Rollback()
	inserted, err := tx.ExecContext(r.Context(), `INSERT INTO entry_imports(fingerprint,created_by) VALUES($1,$2) ON CONFLICT DO NOTHING`, fingerprint, u.ID)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка регистрации импорта")
		return
	}
	count, err := inserted.RowsAffected()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка регистрации импорта")
		return
	}
	if count == 0 {
		middleware.WriteError(w, 409, "этот файл уже импортирован в выбранный раздел")
		return
	}
	for _, row := range result.Rows {
		payload, _ := json.Marshal(row.Payload)
		var id string
		if tx.QueryRowContext(r.Context(), `INSERT INTO entries(category_code,partner_id,period_type,report_year,audience,payload,amount_rub,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, category, partner, period, year, audience, payload, row.Amount, u.ID).Scan(&id) != nil {
			middleware.WriteError(w, 500, "ошибка сохранения; импорт отменён целиком")
			return
		}
		if logAudit(tx, "entry", id, "create", u.ID, "импорт Excel", nil, row) != nil {
			middleware.WriteError(w, 500, "ошибка аудита; импорт отменён")
			return
		}
	}
	if tx.Commit() != nil {
		middleware.WriteError(w, 500, "ошибка сохранения импорта")
		return
	}
	result.Committed = true
	middleware.WriteJSON(w, 201, result)
}

func (h *PartnerHandlers) DirectoryTemplate(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	wb := xlsx.New()
	wb.AddSheet("Справочник", []string{"name", "partner_kind", "region", "source"}, nil)
	wb.AddSheet("Инструкция", []string{"Поле", "Описание"}, [][]interface{}{{"name", "Полное название учебного заведения"}, {"partner_kind", "vuz / kolledj / school"}, {"region", "Регион"}, {"source", "Источник и дата актуальности сведений"}})
	writeWorkbook(w, wb, "education_directory.xlsx")
}
func (h *PartnerHandlers) ImportDirectory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	_, rows, err := uploadedWorkbook(w, r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	if strings.Join(rows[0], "|") != "name|partner_kind|region|source" {
		middleware.WriteError(w, 400, "используйте заголовки из шаблона справочника")
		return
	}
	valid := [][]string{}
	errors := []string{}
	seen := map[string]bool{}
	for i, row := range rows[1:] {
		if strings.Join(row, "") == "" {
			continue
		}
		for len(row) < 4 {
			row = append(row, "")
		}
		if len(row) != 4 || len([]rune(row[0])) < 2 || len([]rune(row[0])) > 1000 || (row[1] != "vuz" && row[1] != "kolledj" && row[1] != "school") || len([]rune(row[2])) > 200 || row[3] == "" || len([]rune(row[3])) > 1000 {
			errors = append(errors, fmt.Sprintf("Строка %d: проверьте название, тип, регион и источник", i+2))
			continue
		}
		key := strings.Join(row[:3], "\x00")
		if seen[key] {
			errors = append(errors, fmt.Sprintf("Строка %d: дубль организации", i+2))
			continue
		}
		seen[key] = true
		valid = append(valid, row)
	}
	if len(valid) == 0 && len(errors) == 0 {
		errors = append(errors, "нет строк данных")
	}
	commit := r.URL.Query().Get("commit") == "1" && len(errors) == 0
	if commit {
		tx, e := h.DB.BeginTx(r.Context(), nil)
		if e != nil {
			middleware.WriteError(w, 500, "ошибка транзакции")
			return
		}
		defer tx.Rollback()
		for _, row := range valid {
			if _, e := tx.ExecContext(r.Context(), `INSERT INTO education_directory(name,partner_kind,region,source) VALUES($1,$2,$3,$4) ON CONFLICT(name,partner_kind,region) DO UPDATE SET source=EXCLUDED.source,updated_at=now()`, row[0], row[1], row[2], row[3]); e != nil {
				middleware.WriteError(w, 500, "импорт отменён")
				return
			}
		}
		if logAudit(tx, "directory", "", "import", u.ID, fmt.Sprintf("%d организаций", len(valid)), nil, nil) != nil {
			middleware.WriteError(w, 500, "ошибка аудита")
			return
		}
		if tx.Commit() != nil {
			middleware.WriteError(w, 500, "ошибка сохранения")
			return
		}
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"count": len(valid), "errors": errors, "committed": commit})
}
