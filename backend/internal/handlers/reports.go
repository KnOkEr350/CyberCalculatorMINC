package handlers

import (
	"cybercalc/internal/calculators"
	"cybercalc/internal/docx"
	"cybercalc/internal/money"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/xlsx"
)

type ReportHandlers struct {
	DB *sql.DB
}

type reportEntryRow struct {
	Eligible              bool
	PartnerID             string
	PartnerName           sql.NullString
	CategoryCode          string
	Audience              string
	AmountRub             money.Amount
	FormulaAmountRub      money.Amount
	CostMethod            string
	Payload               []byte
	AgreementNumber       string
	AgreementKind         string
	AgreementStatus       string
	RegionalAuthorityName string
	LegalEntityGroupID    string
	LegalEntityGroup      string
	InteractionAgreement  string
}

// «Сформировать годовой план активностей» / «отчёт по реализованным
// мероприятиям» / «отчёт по стажировкам» — по каждому партнёру + сводный
// для МЦ (выгрузка в xls). Один эндпоинт параметризован period_type и,
// опционально, category_code (для отчёта по стажировкам — internship,
// employment_practice).
func (h *ReportHandlers) Export(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	q := r.URL.Query()
	year, _ := strconv.Atoi(q.Get("report_year"))
	if year < 2000 || year > 2100 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите report_year")
		return
	}
	if q.Get("report_type") == "annex4" {
		h.exportAnnex4(w, r, u, year)
		return
	}
	if reportType := q.Get("report_type"); reportType != "" {
		h.ExportRegulatory(w, r, u, year, reportType)
		return
	}
	periodType := q.Get("period_type")
	if periodType != "plan" && periodType != "fact" {
		middleware.WriteError(w, http.StatusBadRequest, "period_type должен быть plan или fact")
		return
	}
	categoryFilter := q.Get("category_code") // пусто = все категории (годовой план); можно ограничить, напр. internship

	query := `SELECT COALESCE(e.partner_id::text,''),p.name,e.category_code,e.audience,e.amount_rub,e.formula_amount_rub,e.cost_method,e.payload,eligibility.eligible,
		COALESCE(a.number,''),COALESCE(a.agreement_kind,''),COALESCE(a.status,''),COALESCE(ra.name,''),COALESCE(g.id::text,''),COALESCE(g.name,''),
		CASE WHEN g.id IS NULL THEN '' ELSE concat('от ',to_char(g.interaction_agreement_date,'DD.MM.YYYY'),' № ',g.interaction_agreement_number) END
		FROM entries e JOIN entry_eligibility eligibility ON eligibility.id=e.id LEFT JOIN partners p ON p.id=e.partner_id
		LEFT JOIN agreements a ON a.id=e.agreement_id
		LEFT JOIN regional_authorities ra ON ra.id=a.regional_authority_id
		LEFT JOIN legal_entity_groups g ON g.id=a.legal_entity_group_id
		WHERE e.period_type = $1 AND e.report_year = $2`
	args := []interface{}{periodType, year}
	if company := itCompanyScope(u); company != "" {
		args = append(args, company)
		// Only the positional parameter number is formatted; the company ID stays in args.
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		query += fmt.Sprintf(" AND e.it_company_id::text=$%d", len(args))
	}
	if categoryFilter != "" {
		args = append(args, categoryFilter)
		// Only the positional parameter number is formatted; the category stays in args.
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		query += fmt.Sprintf(" AND e.category_code=$%d", len(args))
	}
	if scope := partnerScope(u, q.Get("partner_id")); scope != "" {
		args = append(args, scope)
		// Only the positional parameter number is formatted into this fixed fragment.
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		query += fmt.Sprintf(" AND e.partner_id::text=$%d", len(args))
	}
	if mentor := q.Get("mentor_id"); mentor != "" {
		args = append(args, mentor)
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		query += fmt.Sprintf(" AND e.payload->>'mentor_id'=$%d", len(args))
	}
	if agreement := q.Get("agreement_id"); agreement != "" {
		args = append(args, agreement)
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		query += fmt.Sprintf(" AND e.agreement_id::text=$%d", len(args))
	}
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query += ` ORDER BY p.name NULLS LAST, e.category_code LIMIT 10001`

	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса")
		return
	}
	defer rows.Close()

	var data []reportEntryRow
	var payloadBytes int
	for rows.Next() {
		var row reportEntryRow
		if err := rows.Scan(&row.PartnerID, &row.PartnerName, &row.CategoryCode, &row.Audience, &row.AmountRub, &row.FormulaAmountRub, &row.CostMethod, &row.Payload, &row.Eligible,
			&row.AgreementNumber, &row.AgreementKind, &row.AgreementStatus, &row.RegionalAuthorityName, &row.LegalEntityGroupID, &row.LegalEntityGroup, &row.InteractionAgreement); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		data = append(data, row)
		payloadBytes += len(row.Payload)
		if payloadBytes > 16<<20 {
			middleware.WriteError(w, 422, "слишком большой отчёт; сузьте фильтры")
			return
		}
		if len(data) > 10000 {
			middleware.WriteError(w, 422, "в отчёте более 10000 записей; выберите партнёра или категорию")
			return
		}
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения отчёта")
		return
	}
	if len(data) == 0 {
		middleware.WriteError(w, http.StatusConflict, "нет данных для утверждённого отчёта")
		return
	}
	for _, row := range data {
		if !row.Eligible {
			middleware.WriteError(w, http.StatusConflict, "выгрузка заблокирована: сначала переведите каждый затронутый отчёт по соглашению из черновика в состояния «Готово», «Проверено» и «Утверждено»")
			return
		}
	}
	categoryNames := map[string]string{}
	categoryRows, err := h.DB.QueryContext(r.Context(), `SELECT code,name FROM activity_categories`)
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	for categoryRows.Next() {
		var code, name string
		if categoryRows.Scan(&code, &name) != nil {
			categoryRows.Close()
			middleware.WriteError(w, 500, "ошибка справочника")
			return
		}
		categoryNames[code] = name
	}
	err = categoryRows.Err()
	categoryRows.Close()
	if err != nil {
		middleware.WriteError(w, 500, "ошибка справочника")
		return
	}
	audiences := map[string]string{"vuz": "Вуз", "kolledj": "СПО", "school": "Школа"}

	wb := xlsx.New()
	headers := []string{"Партнёр", "Соглашение", "Тип / статус соглашения", "РОИВ", "Группа лиц", "Договор о взаимодействии", "Категория активности", "Аудитория", "Метод стоимости", "Сумма по методике, руб.", "Сумма в отчёте, руб.", "Параметры", "Статус отчёта"}
	status := func(d reportEntryRow) string {
		return "Утверждено"
	}

	// Сводный лист для МЦ — все партнёры вместе.
	var consolidated [][]interface{}
	var total money.Amount
	for _, d := range data {
		partnerName := "—"
		if d.PartnerName.Valid {
			partnerName = d.PartnerName.String
		}
		costMethod := map[string]string{"average": "Средние значения", "actual": "Фактические затраты"}[d.CostMethod]
		consolidated = append(consolidated, []interface{}{partnerName, d.AgreementNumber, officeValue(d.AgreementKind) + " / " + officeValue(d.AgreementStatus), d.RegionalAuthorityName, d.LegalEntityGroup, d.InteractionAgreement, categoryNames[d.CategoryCode], audiences[d.Audience], costMethod, d.FormulaAmountRub, d.AmountRub, readablePayload(d.CategoryCode, d.Payload), status(d)})
		var sumErr error
		total, sumErr = money.Add(total, d.AmountRub)
		if sumErr != nil {
			middleware.WriteError(w, 422, sumErr.Error())
			return
		}
	}
	consolidated = append(consolidated, []interface{}{"ИТОГО (только утверждённые данные)", "", "", "", "", "", "", "", "", "", total, "", "Утверждено"})
	wb.AddSheet("Сводный для МЦ", headers, consolidated)
	groupIDs := map[string]bool{}
	for _, row := range data {
		if row.LegalEntityGroupID != "" {
			groupIDs[row.LegalEntityGroupID] = true
		}
	}
	if len(groupIDs) > 0 {
		groupRows := [][]interface{}{}
		for groupID := range groupIDs {
			rows, groupErr := h.DB.QueryContext(r.Context(), `SELECT g.name,g.interaction_agreement_number,g.interaction_agreement_date::text,
				g.authorized_entity_name,g.authorized_entity_inn,g.authorized_entity_ogrn,
				m.name,m.inn,m.ogrn,m.is_it_organization,COALESCE(m.target_amount_rub::text,'')
				FROM legal_entity_groups g JOIN legal_entity_group_members m ON m.group_id=g.id
				WHERE g.id::text=$1 ORDER BY m.is_it_organization DESC,m.name,m.id`, groupID)
			if groupErr != nil {
				middleware.WriteError(w, 500, "ошибка состава группы юридических лиц")
				return
			}
			for rows.Next() {
				var groupName, agreementNumber, agreementDate, authorizedName, authorizedINN, authorizedOGRN string
				var memberName, memberINN, memberOGRN, targetAmount string
				var isITOrganization bool
				if groupErr = rows.Scan(&groupName, &agreementNumber, &agreementDate, &authorizedName, &authorizedINN, &authorizedOGRN,
					&memberName, &memberINN, &memberOGRN, &isITOrganization, &targetAmount); groupErr != nil {
					rows.Close()
					middleware.WriteError(w, 500, "ошибка состава группы юридических лиц")
					return
				}
				groupRows = append(groupRows, []interface{}{groupName, agreementNumber, agreementDate, authorizedName, authorizedINN, authorizedOGRN,
					memberName, memberINN, memberOGRN, map[bool]string{true: "ИТ-организация", false: "Иное юридическое лицо"}[isITOrganization], targetAmount})
			}
			groupErr = rows.Err()
			rows.Close()
			if groupErr != nil {
				middleware.WriteError(w, 500, "ошибка состава группы юридических лиц")
				return
			}
		}
		wb.AddSheet("Группа лиц", []string{"Группа", "Номер договора", "Дата договора", "Уполномоченное лицо", "ИНН уполномоченного", "ОГРН уполномоченного", "Участник", "ИНН участника", "ОГРН участника", "Тип участника", "Целевой объём, руб."}, groupRows)
	}
	if q.Get("format") == "docx" {
		docRows := [][]string{}
		for _, row := range consolidated {
			cells := []string{}
			for _, v := range row {
				cells = append(cells, fmt.Sprint(v))
			}
			docRows = append(docRows, cells)
		}
		body, e := docx.Table(fmt.Sprintf("%s — %d. Суммы в рублях", map[string]string{"plan": "План мероприятий", "fact": "Реализованные мероприятия"}[periodType], year), headers, docRows)
		if e != nil {
			middleware.WriteError(w, 500, "ошибка Word")
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		localizedName := fmt.Sprintf("отчет_%s_%d.docx", map[string]string{"plan": "план", "fact": "факт"}[periodType], year)
		if company := itCompanyScope(u); company != "" {
			h.writeGenerated(w, r, u, "custom", "docx", localizedName, company, q.Get("partner_id"), q.Get("agreement_id"), year, body)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report_%s_%d.docx"; filename*=UTF-8''%s`, periodType, year, url.PathEscape(localizedName)))
		w.Write(body)
		return
	}
	if format := q.Get("format"); format != "" && format != "xlsx" {
		middleware.WriteError(w, 400, "формат должен быть xlsx или docx")
		return
	}
	if categoryFilter != "" {
		if calc, e := calculators.Get(categoryFilter); e == nil {
			fields := []calculators.FieldSpec{}
			cols := []string{"Учебное заведение", "Соглашение", "РОИВ"}
			for _, f := range calc.Fields() {
				if f.Key != "org_name" && f.Key != "mentor_id" {
					fields = append(fields, f)
					cols = append(cols, f.Label)
				}
			}
			cols = append(cols, "Сумма, руб.")
			details := [][]interface{}{}
			for _, d := range data {
				var payload map[string]interface{}
				json.Unmarshal(d.Payload, &payload)
				row := []interface{}{d.PartnerName.String, d.AgreementNumber, d.RegionalAuthorityName}
				for _, f := range fields {
					v, ok := payload[f.Key]
					if !ok {
						v = ""
					}
					if text, ok := v.(string); ok {
						v = officeValue(text)
					}
					row = append(row, v)
				}
				row = append(row, d.AmountRub)
				details = append(details, row)
			}
			wb.AddSheet("Детализация", cols, details)
		}
	}

	// Отдельный лист по каждому партнёру.
	byPartner := map[string][][]interface{}{}
	var order []string
	for _, d := range data {
		partnerName := "Без партнёра"
		if d.PartnerName.Valid {
			partnerName = d.PartnerName.String
		}
		key := d.PartnerID
		if _, ok := byPartner[key]; !ok {
			order = append(order, key)
		}
		costMethod := map[string]string{"average": "Средние значения", "actual": "Фактические затраты"}[d.CostMethod]
		byPartner[key] = append(byPartner[key], []interface{}{partnerName, d.AgreementNumber, officeValue(d.AgreementKind) + " / " + officeValue(d.AgreementStatus), d.RegionalAuthorityName, d.LegalEntityGroup, d.InteractionAgreement, categoryNames[d.CategoryCode], audiences[d.Audience], costMethod, d.FormulaAmountRub, d.AmountRub, readablePayload(d.CategoryCode, d.Payload), status(d)})
	}
	for _, key := range order {
		wb.AddSheet(fmt.Sprint(byPartner[key][0][0]), headers, byPartner[key])
	}

	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка формирования файла")
		return
	}

	filename := fmt.Sprintf("отчет_%s_%d.xlsx", map[string]string{"plan": "план", "fact": "факт"}[periodType], year)
	if company := itCompanyScope(u); company != "" {
		h.writeGenerated(w, r, u, "custom", "xlsx", filename, company, q.Get("partner_id"), q.Get("agreement_id"), year, body)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report_%s_%d.xlsx"; filename*=UTF-8''%s`, periodType, year, url.PathEscape(filename)))
	w.Write(body)

	logAudit(r.Context(), h.DB, "report", "", "export", u.ID, fmt.Sprintf("выгрузка %s", filename), nil, nil)
}

func payloadSummary(raw []byte) string {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	s := ""
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		if s != "" {
			s += "; "
		}
		s += fmt.Sprintf("%s=%v", k, v)
	}
	return s
}

func readablePayload(category string, raw []byte) string {
	calc, err := calculators.Get(category)
	if err != nil {
		return payloadSummary(raw)
	}
	var payload map[string]interface{}
	json.Unmarshal(raw, &payload)
	parts := []string{}
	for _, f := range calc.Fields() {
		if f.Key == "org_name" || f.Key == "mentor_id" {
			continue
		}
		if v, ok := payload[f.Key]; ok && v != nil && fmt.Sprint(v) != "" {
			parts = append(parts, f.Label+": "+officeValue(fmt.Sprint(v)))
		}
	}
	return strings.Join(parts, "; ")
}
