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
	"net/url"
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
		headers = append(headers, f.Label)
		options := make([]string, 0, len(f.Options))
		for _, option := range f.Options {
			options = append(options, officeValue(option))
		}
		info = append(info, []interface{}{f.Label, map[bool]string{true: "Да", false: "Нет"}[f.Required], strings.Join(options, ", ")})
	}
	if r.URL.Query().Get("period_type") == string(models.PeriodFact) {
		headers = append(headers, "Метод расчёта", "Фактическая сумма, руб.")
		info = append(info,
			[]interface{}{"Метод расчёта", "Нет", "Средние значения / Фактические затраты"},
			[]interface{}{"Фактическая сумма, руб.", "Для фактических затрат", "Положительное число; потребуется аудиторское заключение"},
		)
	}
	wb := xlsx.New()
	wb.AddSheet("Данные", headers, nil)
	wb.AddSheet("Инструкция", []string{"Название столбца", "Обязательно", "Допустимые значения"}, info)
	writeWorkbook(w, wb, "шаблон_импорта.xlsx")
}

func officeValueLabel(value string) string {
	return map[string]string{
		"vuz": "Вуз", "kolledj": "Колледж", "school": "Школа",
		"rpd": "РПД", "oop": "ООП", "vo": "Высшее образование", "spo": "Среднее профессиональное образование",
		"development": "Разработка", "update": "Актуализация", "expertise": "Экспертиза",
		"education_organization": "С образовательной организацией", "roiv": "С РОИВ",
		"needs_review": "Требует проверки", "draft": "Проект", "active": "Действует",
		"suspended": "Приостановлено", "expired": "Истекло", "terminated": "Расторгнуто",
	}[value]
}

func officeValue(value string) string {
	if label := officeValueLabel(value); label != "" {
		return label
	}
	return value
}

func canonicalOfficeValue(value string, options []string) string {
	value = strings.TrimSpace(value)
	for _, option := range options {
		if strings.EqualFold(value, option) || strings.EqualFold(value, officeValue(option)) {
			return option
		}
	}
	return value
}
func writeWorkbook(w http.ResponseWriter, wb *xlsx.Workbook, name string) {
	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка создания Excel")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="download.xlsx"; filename*=UTF-8''%s`, url.PathEscape(name)))
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
	Row           int                    `json:"row"`
	Payload       map[string]interface{} `json:"payload"`
	Amount        money.Amount           `json:"amount_rub"`
	FormulaAmount money.Amount           `json:"formula_amount_rub"`
	ActualAmount  *money.Amount          `json:"actual_amount_rub,omitempty"`
	CostMethod    string                 `json:"cost_method"`
}
type importResult struct {
	Rows      []importRow  `json:"rows"`
	Errors    []string     `json:"errors"`
	Total     money.Amount `json:"total_rub"`
	Committed bool         `json:"committed"`
}

func (h *EntryHandlers) Import(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canPrepareReports(u) {
		middleware.WriteError(w, http.StatusForbidden, "импорт плана и отчёта доступен только ИТ-организации")
		return
	}
	q := r.URL.Query()
	category, partner, agreementID, period := q.Get("category_code"), q.Get("partner_id"), q.Get("agreement_id"), q.Get("period_type")
	year, err := strconv.Atoi(q.Get("report_year"))
	if err != nil || year < 2000 || year > 2100 || (period != "plan" && period != "fact") {
		middleware.WriteError(w, 400, "укажите корректные год и план/факт")
		return
	}
	if !requirePartnerTenant(w, r, h.DB, u, partner) {
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
	if err := h.validateAgreementContext(r, agreementID, partner, category, year); err != nil {
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
	specs["cost_method"] = calculators.FieldSpec{Key: "cost_method", Label: "Метод расчёта", Type: "select", Options: []string{"average", "actual"}}
	specs["actual_amount_rub"] = calculators.FieldSpec{Key: "actual_amount_rub", Label: "Фактическая сумма, руб.", Type: "number"}
	labels["Метод расчёта"] = "cost_method"
	labels["Фактическая сумма, руб."] = "actual_amount_rub"
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
			middleware.WriteError(w, 400, "нет обязательного столбца: "+f.Label)
			return
		}
	}
	result := importResult{Rows: []importRow{}, Errors: []string{}}
	calc, _ := calculators.Get(category)
	for i, values := range table[1:] {
		if strings.TrimSpace(strings.Join(values, "")) == "" {
			continue
		}
		row := importRow{Row: i + 2, Payload: map[string]interface{}{"org_name": partner}, CostMethod: "average"}
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
				if key == "cost_method" {
					switch strings.ToLower(value) {
					case "", "average", "средние значения":
						row.CostMethod = "average"
					case "actual", "фактические затраты":
						row.CostMethod = "actual"
					default:
						return fmt.Errorf("метод расчёта: выберите средние значения или фактические затраты")
					}
				} else if key == "actual_amount_rub" {
					if value != "" {
						parsed, e := money.Parse(strings.ReplaceAll(value, ",", "."))
						if e != nil {
							return fmt.Errorf("фактическая сумма: %v", e)
						}
						row.ActualAmount = &parsed
					}
				} else if f.Type == "number" {
					n := strings.ReplaceAll(value, ",", ".")
					_, e := strconv.ParseFloat(n, 64)
					if e != nil {
						return fmt.Errorf("%s: нужно число", f.Label)
					}
					row.Payload[key] = json.Number(n)
				} else {
					row.Payload[key] = canonicalOfficeValue(value, f.Options)
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
			formulaAmount, e := calculators.CalculateAmount(category, models.Audience(audience), row.Payload)
			if e != nil {
				return e
			}
			if e := calculators.ValidateAmount(formulaAmount.Rubles()); e != nil {
				return e
			}
			row.FormulaAmount = formulaAmount
			row.CostMethod, row.Amount, e = resolveEntryAmount(row.CostMethod, row.ActualAmount, formulaAmount)
			if e != nil {
				return e
			}
			if period != string(models.PeriodFact) && row.CostMethod == "actual" {
				return fmt.Errorf("фактические затраты указываются только в отчёте «Факт»")
			}
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
	companyID, ok := requireITCompanyForWrite(w, u)
	if !ok {
		return
	}
	context, _ := json.Marshal([]interface{}{u.ID, category, partner, agreementID, period, year})
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
		if tx.QueryRowContext(r.Context(), `INSERT INTO entries(category_code,partner_id,agreement_id,period_type,report_year,audience,payload,amount_rub,formula_amount_rub,actual_amount_rub,cost_method,it_company_id,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, category, partner, agreementID, period, year, audience, payload, row.Amount, row.FormulaAmount, row.ActualAmount, row.CostMethod, companyID, u.ID).Scan(&id) != nil {
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
	if !canReviewEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к справочнику учебных заведений")
		return
	}
	wb := xlsx.New()
	headers := make([]string, 0, len(directoryColumns))
	for _, column := range directoryColumns {
		headers = append(headers, column.Label)
	}
	headers = append(headers, "Коды направлений", "Источник направлений", "Действие")
	rows, err := h.DB.QueryContext(r.Context(), `SELECT name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
		COALESCE(license_number,''),license_status,institution_status,COALESCE(registry_record_id,''),
		COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),array_to_string(program_codes,','),programs_source_url
		FROM education_directory ORDER BY name,id LIMIT 10000`)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка выгрузки справочника")
		return
	}
	defer rows.Close()
	data := [][]interface{}{}
	for rows.Next() {
		var values [13]string
		if err := rows.Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5],
			&values[6], &values[7], &values[8], &values[9], &values[10], &values[11], &values[12]); err != nil {
			middleware.WriteError(w, 500, "ошибка чтения справочника")
			return
		}
		data = append(data, []interface{}{values[0], officeValue(values[1]), values[2], values[3], values[4], values[5],
			directoryOfficeValue("license_status", values[6]), directoryOfficeValue("institution_status", values[7]),
			values[8], values[9], values[10], values[11], values[12], ""})
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения справочника")
		return
	}
	wb.AddSheet("Справочник", headers, data)
	wb.AddSheet("Инструкция", []string{"Название столбца", "Описание"}, [][]interface{}{
		{"Наименование", "Полное наименование из реестра лицензий"},
		{"Тип учебного заведения", "Вуз / Колледж / Школа"},
		{"Регион", "Субъект Российской Федерации"},
		{"ИНН", "ИНН с корректной контрольной суммой"},
		{"ОГРН / ОГРНИП", "ОГРН/ОГРНИП с корректной контрольной суммой"},
		{"Номер лицензии", "Регистрационный номер лицензии"},
		{"Статус лицензии", "Действует / Приостановлена / Истекла / Аннулирована"},
		{"Статус организации", "Действует / Не действует / Реорганизована / Ликвидирована"},
		{"Идентификатор записи реестра", "Уникальный идентификатор записи официального реестра"},
		{"Ссылка на официальный источник", "HTTPS-ссылка на Рособрнадзор или официальный домен *.gov.ru"},
		{"Дата актуальности сведений", "Дата в формате ГГГГ-ММ-ДД"},
		{"Коды направлений", "Точные коды через запятую, например 09.03.01,38.03.05. Вузы показываются при совпадении хотя бы одного кода с приказом № 27"},
		{"Источник направлений", "HTTPS-ссылка на сведения об образовательных программах вуза или реестра"},
		{"Действие", "Для изменённых и проверенных строк укажите «Подтвердить». Пустые строки не импортируются"},
	})
	writeWorkbook(w, wb, "справочник_учебных_заведений.xlsx")
}

func directoryOfficeValue(column, value string) string {
	labels := map[string]map[string]string{
		"license_status":     {"active": "Действует", "suspended": "Приостановлена", "expired": "Истекла", "revoked": "Аннулирована", "unknown": "Не указан"},
		"institution_status": {"active": "Действует", "inactive": "Не действует", "reorganized": "Реорганизована", "liquidated": "Ликвидирована", "unknown": "Не указан"},
	}
	if label := labels[column][value]; label != "" {
		return label
	}
	return value
}

func (h *PartnerHandlers) ImportDirectory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canReviewEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "нет доступа к проверке учебных заведений")
		return
	}
	_, rows, err := uploadedWorkbook(w, r)
	if err != nil {
		middleware.WriteError(w, 400, err.Error())
		return
	}
	valid, errors := validateDirectoryRows(rows)
	commit := r.URL.Query().Get("commit") == "1" && len(errors) == 0
	if commit && !canApproveEducationDirectory(u) {
		middleware.WriteError(w, http.StatusForbidden, "подтверждать массовую загрузку справочника может только администратор")
		return
	}
	if commit {
		tx, e := h.DB.BeginTx(r.Context(), nil)
		if e != nil {
			middleware.WriteError(w, 500, "ошибка транзакции")
			return
		}
		defer tx.Rollback()
		if e := upsertDirectoryRows(r.Context(), tx, valid, "verified", u.ID, true); e != nil {
			middleware.WriteError(w, 500, "импорт отменён: конфликт записи реестра")
			return
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
