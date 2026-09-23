package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"cybercalc/internal/compliance"
	"cybercalc/internal/docx"
	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"cybercalc/internal/platform/activityprojection"
	"cybercalc/internal/xlsx"
	"github.com/lib/pq"
)

type regulatoryRow struct {
	PartnerID, Partner, AgreementID, Agreement, Category, CategoryName, Period, Audience string
	Amount                                                                               money.Amount
	Payload                                                                              []byte
	Eligible                                                                             bool
	Documents                                                                            []string
	RiskState                                                                            string
}

var regulatoryHeaders = map[string][]string{
	// Графы Приложения № 1 к Отчёту: форма заполняется отдельно по каждой
	// ОО или РОИВ и только по реализованным мероприятиям.
	"annex1": {"№ п/п", "Вид мероприятия", "Мероприятие", "Метрика (основная)", "Наименование показателя", "Значение показателя", "Стоимость, тыс. руб.", "Сумма затрат по мероприятию, тыс. руб.", "Дополнительная информация"},
	// Графы Приложения № 2 к Отчёту: форма строится по программам
	// стажировок, а не по отдельным стажёрам.
	"annex2": {"№ п/п", "Наименование программы стажировок", "Метрика (основная)", "Наименование показателя", "Значение показателя", "Стоимость, тыс. руб.", "Сумма затрат по программе стажировок, тыс. руб.", "Дополнительная информация"},
	// Специализированный срез «Отчёт по наставникам» из ТЗ (п. 9.2).
	"annex2_mentors": {"№ п/п", "Наставник", "ОО", "Закреплённых стажёров", "Часы сопровождения", "Сумма затрат, тыс. руб."},
	// Состав Таблицы 1 Приложения № 5 к Приказу № 270: контрагент, его
	// соглашения, единый для компании норматив 3% сэкономленных льгот и доля
	// контрагента в нём. Официальная форма не содержит колонки плана.
	"annex5":    {"№", "ОО или РОИВ", "Реквизиты соглашений", "3% от объёма сэкономленных средств, тыс. руб.", "Сумма затрат, тыс. руб.", "Процент от норматива, %"},
	"plan_fact": {"Партнёр", "Вид мероприятия", "План, руб.", "Факт, руб.", "Дельта, руб.", "Дельта, %"},
}

func (h *ReportHandlers) ListGenerated(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	company := itCompanyScope(u)
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id,report_type,file_format,report_year,COALESCE(partner_id::text,''),
		COALESCE(agreement_id::text,''),file_name,content_sha256,size_bytes,filters,generated_by,generated_at
		FROM generated_reports WHERE ($1='' OR it_company_id::text=$1) ORDER BY generated_at DESC LIMIT 200`, company)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось загрузить реестр отчётов")
		return
	}
	defer rows.Close()
	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, kind, format, partner, agreement, name, hash, actor string
		var year int
		var size int64
		var filters json.RawMessage
		var generated interface{}
		if rows.Scan(&id, &kind, &format, &year, &partner, &agreement, &name, &hash, &size, &filters, &actor, &generated) != nil {
			middleware.WriteError(w, 500, "не удалось прочитать реестр отчётов")
			return
		}
		items = append(items, map[string]interface{}{"id": id, "report_type": kind, "file_format": format, "report_year": year, "partner_id": partner, "agreement_id": agreement, "file_name": name, "content_sha256": hash, "size_bytes": size, "filters": filters, "generated_by": actor, "generated_at": generated})
	}
	middleware.WriteJSON(w, 200, items)
}

// reportFilters собирает набор применённых параметров выгрузки из пар
// ключ/значение, отбрасывая пустые значения, чтобы в реестре хранились
// только реально применённые фильтры, а не пустые плейсхолдеры.
func reportFilters(pairs ...string) map[string]string {
	filters := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if value := pairs[i+1]; value != "" {
			filters[pairs[i]] = value
		}
	}
	return filters
}

func regulatoryRiskState(row regulatoryRow) string {
	payload := map[string]interface{}{}
	_ = json.Unmarshal(row.Payload, &payload)
	return riskBucketState(compliance.Evaluate(row.Category, row.Period, payload, row.Documents).State, row.Eligible)
}

func filterRegulatoryRowsByRisk(rows []regulatoryRow, state string) []regulatoryRow {
	out := make([]regulatoryRow, 0, len(rows))
	for _, row := range rows {
		if row.RiskState == state {
			out = append(out, row)
		}
	}
	return out
}

// writeGenerated регистрирует сформированный файл в неизменяемом реестре
// generated_reports (REPORT-11): автор, полный набор применённых
// параметров (partner_id/agreement_id уходят в отдельные FK-колонки для
// выборки, остальные — в JSONB filters), hash и время. Повторное скачивание
// того же файла отдаёт зафиксированные байты через DownloadGenerated.
func (h *ReportHandlers) writeGenerated(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, kind, format, filename, company string, filters map[string]string, year int, body []byte) {
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	partner, agreement := filters["partner_id"], filters["agreement_id"]
	filtersJSON, _ := json.Marshal(filters)
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO generated_reports(it_company_id,report_type,file_format,report_year,partner_id,agreement_id,file_name,content_sha256,size_bytes,content_bytes,filters,generated_by)
		VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12)`, company, kind, format, year, partner, agreement, filename, hash, len(body), body, filtersJSON, u.ID); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось зарегистрировать сформированный файл")
		return
	}
	if format == "docx" {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	} else if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report.%s"; filename*=UTF-8''%s`, format, url.PathEscape(filename)))
	w.Header().Set("X-Content-SHA256", hash)
	_, _ = w.Write(body)
}

func (h *ReportHandlers) DownloadGenerated(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, id string) {
	var format, name, hash string
	var body []byte
	err := h.DB.QueryRowContext(r.Context(), `SELECT file_format,file_name,content_sha256,content_bytes FROM generated_reports WHERE id::text=$1 AND ($2='' OR it_company_id::text=$2)`, id, itCompanyScope(u)).Scan(&format, &name, &hash, &body)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "файл не найден")
		return
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать файл")
		return
	}
	if format == "docx" {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	} else if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report.%s"; filename*=UTF-8''%s`, format, url.PathEscape(name)))
	w.Header().Set("X-Content-SHA256", hash)
	_, _ = w.Write(body)
}

func (h *ReportHandlers) ExportRegulatory(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, kind string) {
	company := itCompanyScope(u)
	partner := strings.TrimSpace(r.URL.Query().Get("partner_id"))
	agreement := strings.TrimSpace(r.URL.Query().Get("agreement_id"))
	riskFilter := strings.TrimSpace(r.URL.Query().Get("risk_filter"))
	if company == "" {
		middleware.WriteError(w, 400, "для отчёта назначьте ИТ-компанию")
		return
	}
	if riskFilter != "" && riskFilter != "green" && riskFilter != "yellow" && riskFilter != "red" {
		middleware.WriteError(w, 400, "risk_filter должен быть green, yellow или red")
		return
	}
	if riskFilter != "" && kind != "plan_fact" {
		middleware.WriteError(w, 400, "risk_filter доступен только для конструктора план-факт-дельта")
		return
	}
	if kind == "agreement2" || kind == "agreement3" {
		h.exportAgreementTemplate(w, r, u, year, kind, company, partner, agreement)
		return
	}
	if kind == "annex3" {
		h.exportAbsenceStatements(w, r, u, year, company, partner, agreement)
		return
	}
	if kind == "plan_fact" && h.Projection != nil {
		h.exportPlanFactProjection(w, r, u, year, company, partner, agreement, riskFilter)
		return
	}
	conds := []string{"e.it_company_id::text=$1", "e.report_year=$2", "eligibility.eligible"}
	args := []interface{}{company, year}
	if partner != "" {
		args = append(args, partner)
		// Only the positional parameter number is formatted; the partner ID stays in args.
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		conds = append(conds, fmt.Sprintf("e.partner_id::text=$%d", len(args)))
	}
	if agreement != "" {
		args = append(args, agreement)
		// Only the positional parameter number is formatted; the agreement ID stays in args.
		// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
		conds = append(conds, fmt.Sprintf("e.agreement_id::text=$%d", len(args)))
	}
	if kind == "annex2" {
		conds = append(conds, "e.category_code IN ('internship','employment_practice')")
	}
	if kind == "annex1" || kind == "annex3" {
		// Приложения № 1 и № 3 к Отчёту говорят о реализованных
		// мероприятиях, поэтому план в них не попадает.
		conds = append(conds, "e.period_type='fact'")
	}
	// conds contains only fixed SQL fragments and $N placeholders; request values are in args.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT COALESCE(e.partner_id::text,''),COALESCE(p.name,''),COALESCE(e.agreement_id::text,''),COALESCE(a.number,''),e.category_code,c.name,e.period_type,e.audience,e.amount_rub,e.payload,eligibility.eligible,
		ARRAY(SELECT DISTINCT att.document_type||':'||att.review_status FROM attachments att WHERE att.entry_id=e.id AND att.retention_expires_at>now())
		FROM entries e JOIN entry_eligibility eligibility ON eligibility.id=e.id LEFT JOIN partners p ON p.id=e.partner_id LEFT JOIN agreements a ON a.id=e.agreement_id JOIN activity_categories c ON c.code=e.category_code WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY p.name,c.name,e.period_type,e.id`
	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось собрать отчёт")
		return
	}
	defer rows.Close()
	data := []regulatoryRow{}
	for rows.Next() {
		var item regulatoryRow
		var documents pq.StringArray
		if rows.Scan(&item.PartnerID, &item.Partner, &item.AgreementID, &item.Agreement, &item.Category, &item.CategoryName, &item.Period, &item.Audience, &item.Amount, &item.Payload, &item.Eligible, &documents) != nil {
			middleware.WriteError(w, 500, "не удалось прочитать отчёт")
			return
		}
		item.Documents = []string(documents)
		item.RiskState = regulatoryRiskState(item)
		data = append(data, item)
	}
	if riskFilter != "" {
		data = filterRegulatoryRowsByRisk(data, riskFilter)
	}
	if len(data) == 0 {
		middleware.WriteError(w, 409, "нет утверждённых данных для формы")
		return
	}
	outputFormat := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if outputFormat == "" {
		outputFormat = "xlsx"
	}
	if outputFormat != "xlsx" && outputFormat != "csv" {
		middleware.WriteError(w, 400, "format должен быть xlsx или csv")
		return
	}
	if outputFormat == "csv" && kind != "plan_fact" {
		middleware.WriteError(w, 400, "CSV доступен только для конструктора план-факт-дельта")
		return
	}
	wb := xlsx.New()
	filename := ""
	switch kind {
	case "annex1":
		for _, sheet := range buildAnnex1Sheets(data) {
			wb.AddSheet(sheet.Name, regulatoryHeaders["annex1"], sheet.Rows)
		}
		filename = fmt.Sprintf("приложение_1_%d.xlsx", year)
	case "annex2":
		// Режим «Отчёт по наставникам» — тот же набор данных в разрезе
		// наставников (ТЗ, п. 9.2).
		if strings.TrimSpace(r.URL.Query().Get("mode")) == "mentors" {
			wb.AddSheet("Отчёт по наставникам", regulatoryHeaders["annex2_mentors"], buildMentorRows(data))
			filename = fmt.Sprintf("отчет_по_наставникам_%d.xlsx", year)
			break
		}
		wb.AddSheet("Приложение № 2", regulatoryHeaders["annex2"], buildAnnex2Rows(data))
		filename = fmt.Sprintf("приложение_2_%d.xlsx", year)
	case "annex5":
		var target sql.NullString
		_ = h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub::text FROM organization_budget_targets WHERE it_company_id::text=$1 AND report_year=$2`, company, year).Scan(&target)
		targetAmount, _ := money.Parse(target.String) // пустая/некорректная база 3% -> 0, процент по норме покажет "—"
		out, planTotal, factTotal := buildAnnex5Rows(data, targetAmount)
		wb.AddSheet("Приложение № 5", regulatoryHeaders["annex5"], out)
		wb.AddSheet("Норматив 3%", []string{"Год", "Норматив, руб.", "План, руб.", "Факт, руб."}, [][]interface{}{{year, target.String, planTotal, factTotal}})
		filename = fmt.Sprintf("приложение_5_%d.xlsx", year)
	case "plan_fact":
		out := buildPlanFactRows(data)
		if outputFormat == "csv" {
			body, err := tableCSV(regulatoryHeaders["plan_fact"], out)
			if err != nil {
				middleware.WriteError(w, 500, "не удалось сформировать CSV")
				return
			}
			h.writeGenerated(w, r, u, kind, "csv", fmt.Sprintf("план_факт_дельта_%d.csv", year), company,
				reportFilters("partner_id", partner, "agreement_id", agreement, "mode", strings.TrimSpace(r.URL.Query().Get("mode")), "risk_filter", riskFilter, "format", "csv"), year, body)
			return
		}
		wb.AddSheet("План-Факт-Дельта", regulatoryHeaders["plan_fact"], out)
		filename = fmt.Sprintf("план_факт_дельта_%d.xlsx", year)
	default:
		middleware.WriteError(w, 400, "неизвестный тип регламентного отчёта")
		return
	}
	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать Excel")
		return
	}
	h.writeGenerated(w, r, u, kind, "xlsx", filename, company,
		reportFilters("partner_id", partner, "agreement_id", agreement, "mode", strings.TrimSpace(r.URL.Query().Get("mode")), "risk_filter", riskFilter), year, body)
}

func (h *ReportHandlers) exportPlanFactProjection(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, company, partner, agreement, riskFilter string) {
	items, err := h.Projection.List(r.Context(), activityprojection.Filter{
		ReportYear: year, TenantID: company, PartnerID: partner, AgreementID: agreement,
	})
	if err != nil {
		middleware.WriteError(w, 500, "не удалось собрать аналитическую проекцию")
		return
	}
	filtered := make([]activityprojection.Contribution, 0, len(items))
	for _, item := range items {
		if !item.Eligibility.Passed {
			continue
		}
		state := item.Risk.State
		if state != "green" && state != "yellow" && state != "red" {
			state = "red"
		}
		if riskFilter != "" && state != riskFilter {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		middleware.WriteError(w, 409, "нет утверждённых данных для формы")
		return
	}
	groups, err := activityprojection.AggregateContributions(filtered, activityprojection.Grouping{Partner: true, Category: true})
	if err != nil {
		middleware.WriteError(w, 422, err.Error())
		return
	}
	partnerNames, categoryNames, err := h.projectionLabels(r, company)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать справочники аналитики")
		return
	}
	out := buildPlanFactProjectionRows(groups, partnerNames, categoryNames)
	outputFormat := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if outputFormat == "" {
		outputFormat = "xlsx"
	}
	if outputFormat != "xlsx" && outputFormat != "csv" {
		middleware.WriteError(w, 400, "format должен быть xlsx или csv")
		return
	}
	filters := reportFilters("partner_id", partner, "agreement_id", agreement, "mode", strings.TrimSpace(r.URL.Query().Get("mode")), "risk_filter", riskFilter)
	if outputFormat == "csv" {
		body, csvErr := tableCSV(regulatoryHeaders["plan_fact"], out)
		if csvErr != nil {
			middleware.WriteError(w, 500, "не удалось сформировать CSV")
			return
		}
		h.writeGenerated(w, r, u, "plan_fact", "csv", fmt.Sprintf("план_факт_дельта_%d.csv", year), company, reportFilters(
			"partner_id", partner, "agreement_id", agreement, "mode", strings.TrimSpace(r.URL.Query().Get("mode")), "risk_filter", riskFilter, "format", "csv",
		), year, body)
		return
	}
	wb := xlsx.New()
	wb.AddSheet("План-Факт-Дельта", regulatoryHeaders["plan_fact"], out)
	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать Excel")
		return
	}
	h.writeGenerated(w, r, u, "plan_fact", "xlsx", fmt.Sprintf("план_факт_дельта_%d.xlsx", year), company, filters, year, body)
}

func (h *ReportHandlers) projectionLabels(r *http.Request, company string) (map[string]string, map[string]string, error) {
	partnerNames := make(map[string]string)
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id::text,name FROM partners WHERE it_company_id::text=$1`, company)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id, name string
		if err = rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		partnerNames[id] = name
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	categoryNames := make(map[string]string)
	rows, err = h.DB.QueryContext(r.Context(), `SELECT code,name FROM activity_categories`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code, name string
		if err = rows.Scan(&code, &name); err != nil {
			return nil, nil, err
		}
		categoryNames[code] = name
	}
	return partnerNames, categoryNames, rows.Err()
}

type absenceAgreement struct {
	PartnerName string
	SignedOn    string
	Number      string
}

func buildAbsenceStatementRows(items []absenceAgreement) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			formatAbsenceStatement(item.PartnerName, item.SignedOn, item.Number),
			"Фактически подтверждённые мероприятия отсутствуют",
		})
	}
	return rows
}

func (h *ReportHandlers) exportAbsenceStatements(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, company, partner, agreement string) {
	if partner == "" {
		middleware.WriteError(w, 400, "выберите партнёра")
		return
	}
	rows, err := h.DB.QueryContext(r.Context(), `SELECT p.name,a.signed_on::text,a.number
		FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id JOIN partners p ON p.id=ap.partner_id
		WHERE p.id::text=$1 AND a.it_company_id::text=$2 AND ($3='' OR a.id::text=$3)
		AND NOT EXISTS(
			SELECT 1 FROM entries e JOIN entry_eligibility eligibility ON eligibility.id=e.id
			WHERE e.it_company_id::text=$2 AND e.partner_id=p.id AND e.agreement_id=a.id
				AND e.report_year=$4 AND e.period_type='fact' AND eligibility.eligible
		)
		ORDER BY a.signed_on,a.number,a.id`, partner, company, agreement, year)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось проверить соглашения для справки")
		return
	}
	defer rows.Close()
	items := []absenceAgreement{}
	for rows.Next() {
		var item absenceAgreement
		if err := rows.Scan(&item.PartnerName, &item.SignedOn, &item.Number); err != nil {
			middleware.WriteError(w, 500, "не удалось прочитать соглашения для справки")
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "не удалось прочитать соглашения для справки")
		return
	}
	if len(items) == 0 {
		middleware.WriteError(w, 409, "справка об отсутствии недоступна: выбранные соглашения не найдены или по ним есть мероприятия")
		return
	}
	body, err := docx.Table(
		fmt.Sprintf("Приложение № 3. Информационная справка об отсутствии реализованных мероприятий за %d год", year),
		[]string{"Соглашение", "Основание"},
		buildAbsenceStatementRows(items),
	)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать справку")
		return
	}
	h.writeGenerated(w, r, u, "annex3", "docx", fmt.Sprintf("приложение_3_%d.docx", year), company,
		reportFilters("partner_id", partner, "agreement_id", agreement), year, body)
}

func (h *ReportHandlers) exportAgreementTemplate(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, kind, company, partner, agreement string) {
	if agreement == "" || partner == "" {
		middleware.WriteError(w, 400, "выберите партнёра и соглашение")
		return
	}
	var number, signed, partnerName, partnerINN, companyName, companyINN, companyOGRN, address, director string
	var companyAuthority, counterpartySigner, counterpartyPosition, counterpartyAuthority string
	err := h.DB.QueryRowContext(r.Context(), `SELECT a.number,a.signed_on::text,p.name,COALESCE(d.inn,''),c.name,c.inn,c.ogrn,c.legal_address,COALESCE(NULLIF(a.signed_by,''),c.director_name),
		COALESCE(a.company_signer_authority,''),COALESCE(a.counterparty_signer_name,''),COALESCE(a.counterparty_signer_position,''),COALESCE(a.counterparty_signer_authority,'')
		FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id JOIN partners p ON p.id=ap.partner_id LEFT JOIN education_directory d ON d.id=p.directory_id JOIN accredited_it_companies c ON c.id=a.it_company_id WHERE a.id::text=$1 AND p.id::text=$2 AND c.id::text=$3`, agreement, partner, company).Scan(&number, &signed, &partnerName, &partnerINN, &companyName, &companyINN, &companyOGRN, &address, &director, &companyAuthority, &counterpartySigner, &counterpartyPosition, &counterpartyAuthority)
	if err != nil {
		middleware.WriteError(w, 404, "соглашение не найдено")
		return
	}
	party := agreementParty{
		Kind: kind, Number: number, SignedOn: signed, Year: year,
		PartnerName: partnerName, PartnerINN: partnerINN,
		CompanyName: companyName, CompanyINN: companyINN, CompanyOGRN: companyOGRN,
		CompanyAddress: address, CompanyDirector: director, CompanySignerAuthority: companyAuthority,
		CounterpartySigner: counterpartySigner, CounterpartySignerPosition: counterpartyPosition, CounterpartySignerAuthority: counterpartyAuthority,
	}
	body, err := docx.Document(agreementTemplateTitle(kind), agreementTemplateBlocks(party))
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать типовое соглашение")
		return
	}
	h.writeGenerated(w, r, u, kind, "docx", fmt.Sprintf("типовое_соглашение_%s_%d.docx", strings.TrimPrefix(kind, "agreement"), year), company, reportFilters("partner_id", partner, "agreement_id", agreement), year, body)
}

// agreementParty — реквизиты сторон для примерной формы соглашения
// (Приложения № 2 и № 3 к Порядку, утверждённому Приказом № 270).
type agreementParty struct {
	Kind, Number, SignedOn               string
	Year                                 int
	PartnerName, PartnerINN              string
	CompanyName, CompanyINN, CompanyOGRN string
	CompanyAddress, CompanyDirector      string
	CompanySignerAuthority               string
	CounterpartySigner                   string
	CounterpartySignerPosition           string
	CounterpartySignerAuthority          string
}

func agreementTemplateTitle(kind string) string {
	if kind == "agreement3" {
		return "СОГЛАШЕНИЕ об оказании содействия в организации внеурочной деятельности (Приложение № 3 к Порядку)"
	}
	return "СОГЛАШЕНИЕ об оказании содействия в реализации образовательных программ (Приложение № 2 к Порядку)"
}

// agreementTemplateBlocks заполняет примерную форму соглашения: стороны, их
// реквизиты и подписанты. Форма Приказа — это связный текст «в лице …,
// действующего на основании …», а не таблица реквизитов.
func agreementTemplateBlocks(party agreementParty) []docx.Block {
	counterparty := "Образовательная организация"
	if party.Kind == "agreement3" {
		counterparty = "Исполнительный орган субъекта Российской Федерации"
	}
	value := func(s, placeholder string) string {
		if strings.TrimSpace(s) == "" {
			return placeholder
		}
		return s
	}
	return []docx.Block{
		docx.Paragraph(fmt.Sprintf("№ %s от %s", value(party.Number, "____"), formatRuDate(party.SignedOn))),
		docx.Paragraph(""),
		docx.Paragraph(fmt.Sprintf(
			"%s, именуемое в дальнейшем «%s», в лице %s, действующего на основании %s, с одной стороны, и %s, именуемое в дальнейшем «Организация», в лице %s, действующего на основании %s, с другой стороны, совместно именуемые «Стороны», заключили настоящее Соглашение о нижеследующем.",
			value(party.PartnerName, "____________________"), counterparty, value(party.CounterpartySigner, "____________________"), value(party.CounterpartySignerAuthority, "____________________"),
			value(party.CompanyName, "____________________"), value(party.CompanyDirector, "____________________"), value(party.CompanySignerAuthority, "____________________"))),
		docx.Paragraph(""),
		docx.Heading("1. Предмет Соглашения"),
		docx.Paragraph(fmt.Sprintf(
			"Организация оказывает содействие в реализации образовательных программ и организации внеурочной деятельности в %d году в порядке, установленном приказом Минцифры России от 31 марта 2026 г. № 270. Перечень, объём, сроки и условия реализации мероприятий определяются приложениями к настоящему Соглашению.",
			party.Year)),
		docx.Paragraph(""),
		docx.Heading("2. Реквизиты и подписи Сторон"),
		docx.Paragraph(fmt.Sprintf("%s: %s", counterparty, value(party.PartnerName, "____________________"))),
		docx.Paragraph(fmt.Sprintf("ИНН: %s", value(party.PartnerINN, "__________"))),
		docx.Paragraph("Адрес в пределах места нахождения: ____________________"),
		docx.Paragraph(fmt.Sprintf("%s ____________________ / %s", value(party.CounterpartySignerPosition, "Руководитель"), value(party.CounterpartySigner, "____________________"))),
		docx.Paragraph(""),
		docx.Paragraph(fmt.Sprintf("Организация: %s", value(party.CompanyName, "____________________"))),
		docx.Paragraph(fmt.Sprintf("ИНН: %s, ОГРН: %s", value(party.CompanyINN, "__________"), value(party.CompanyOGRN, "_____________"))),
		docx.Paragraph(fmt.Sprintf("Адрес в пределах места нахождения: %s", value(party.CompanyAddress, "____________________"))),
		docx.Paragraph(fmt.Sprintf("Руководитель ____________________ / %s", value(party.CompanyDirector, "____________________"))),
	}
}

// formatRuDate переводит ISO-дату (YYYY-MM-DD, как её отдаёт Postgres) в
// формат ДД.ММ.ГГГГ, принятый в формах Приказа № 270. Нераспознанное
// значение возвращается как есть, чтобы справка не терялась из-за формата.
func formatRuDate(iso string) string {
	parsed, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return parsed.Format("02.01.2006")
}

// formatAbsenceStatement строит формулировку Приложения № 3 к Отчёту:
// «Соглашение с <ОО> от <дата> № <номер>» — вместо сырых идентификаторов
// партнёра и соглашения.
func formatAbsenceStatement(partnerName, signedOn, number string) string {
	return fmt.Sprintf("Соглашение с %s от %s № %s", partnerName, formatRuDate(signedOn), number)
}

// buildAnnex5Rows сводит Таблицу 1 Приложения № 5 к Приказу № 270: по
// каждому контрагенту — реквизиты его соглашений и фактически
// подтверждённые затраты (тыс. руб.), а процент считается от ЕДИНОГО для
// всей компании норматива 3% сэкономленных льгот (target), а не от
// собственного плана контрагента — официальная форма плана вообще не
// содержит. Контрагенты без фактических затрат (пустые строки) программно
// удаляются независимо от настроек фильтрации в UI. planTotal/factTotal —
// сводка для информационного листа «Норматив 3%», не часть самой формы.
func buildAnnex5Rows(data []regulatoryRow, target money.Amount) (out [][]interface{}, planTotal, factTotal money.Amount) {
	type agg struct {
		fact       money.Amount
		agreements []string
		seen       map[string]bool
	}
	totals := map[string]*agg{}
	names := map[string]string{}
	for _, row := range data {
		if totals[row.PartnerID] == nil {
			totals[row.PartnerID] = &agg{seen: map[string]bool{}}
		}
		names[row.PartnerID] = row.Partner
		a := totals[row.PartnerID]
		if row.Agreement != "" && !a.seen[row.Agreement] {
			a.seen[row.Agreement] = true
			a.agreements = append(a.agreements, row.Agreement)
		}
		if row.Period == "plan" {
			planTotal, _ = money.Add(planTotal, row.Amount)
		} else {
			a.fact, _ = money.Add(a.fact, row.Amount)
			factTotal, _ = money.Add(factTotal, row.Amount)
		}
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out = [][]interface{}{}
	targetThousandRub := thousandRub(target)
	number := 0
	for _, key := range keys {
		a := totals[key]
		if a.fact == 0 {
			continue
		}
		var percent interface{} = "—"
		if target > 0 {
			// Округление до сотых процента: без него в ячейку формы
			// попадает хвост двоичной дроби вида 33.333333333333336.
			percent = math.Round(float64(a.fact)/float64(target)*100*100) / 100
		}
		number++
		out = append(out, []interface{}{
			number,
			names[key],
			strings.Join(a.agreements, "; "),
			targetThousandRub,
			thousandRub(a.fact),
			percent,
		})
	}
	if len(out) > 0 {
		var totalPercent interface{} = "—"
		if target > 0 {
			totalPercent = math.Round(float64(factTotal)/float64(target)*100*100) / 100
		}
		out = append(out, []interface{}{"", "ИТОГО", "", targetThousandRub, thousandRub(factTotal), totalPercent})
		out = appendSignatureBlock(out, len(regulatoryHeaders["annex5"]))
	}
	return out, planTotal, factTotal
}

// thousandRub переводит сумму в тыс. руб. — единицу, в которой напечатаны
// формы Приказа № 270.
func thousandRub(amount money.Amount) float64 {
	return math.Round(float64(amount)/1000) / 100
}

func tableCSV(headers []string, rows [][]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // Excel-friendly UTF-8 BOM.
	writer := csv.NewWriter(&buf)
	writer.Comma = ';'
	if err := writer.Write(headers); err != nil {
		return nil, err
	}
	for _, row := range rows {
		record := make([]string, len(row))
		for i, value := range row {
			switch v := value.(type) {
			case money.Amount:
				record[i] = v.String()
			case float64:
				record[i] = fmt.Sprintf("%.2f", v)
			default:
				record[i] = fmt.Sprint(v)
			}
		}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return buf.Bytes(), writer.Error()
}

func appendSignatureBlock(rows [][]interface{}, width int) [][]interface{} {
	if len(rows) == 0 {
		return rows
	}
	if width < 4 {
		width = 4
	}
	makeRow := func(values map[int]interface{}) []interface{} {
		row := make([]interface{}, width)
		for i := range row {
			row[i] = ""
		}
		for index, value := range values {
			if index >= 0 && index < width {
				row[index] = value
			}
		}
		return row
	}
	return append(rows,
		makeRow(nil),
		makeRow(map[int]interface{}{1: "Подписной блок", width - 1: "Дата подписания: ____.__.____"}),
		makeRow(map[int]interface{}{1: "Организация", 2: "Уполномоченное лицо", width - 1: "Подпись / расшифровка"}),
		makeRow(map[int]interface{}{1: "ОО / РОИВ", 2: "Уполномоченное лицо", width - 1: "Подпись / расшифровка"}),
	)
}

// annex1Sheet — один лист Приложения № 1: форма заполняется отдельно по
// каждой ОО или РОИВ, поэтому лист называется именем контрагента.
type annex1Sheet struct {
	Name string
	Rows [][]interface{}
}

// annex1Indicator — подпись показателя объёма для каждого вида мероприятия.
// Сама метрика (единица измерения) берётся из activityMetrics, чтобы графы
// «Метрика» и «Наименование показателя» не дублировали друг друга.
var annex1Indicator = map[string]string{
	"teachers":            "Часы преподавания",
	"internship":          "Часы стажировки",
	"employment_practice": "Часы практики",
	"ood_rpd":             "Разработанные и актуализированные документы",
	"top_it":              "Средства, фактически списанные вузом",
	"it_clubs":            "Часы занятий и разработанные программы",
	"teacher_training":    "Часы обучения учителей",
	"edu_content":         "Месяцы доступа к платформам",
	"minc_decision":       "Объём по решению Минцифры",
}

// activityTitle — графа «Мероприятие»: конкретное наименование из реквизитов
// записи, а не общий вид мероприятия.
func activityTitle(row regulatoryRow, payload map[string]interface{}) string {
	for _, key := range []string{"program_name", "course_name", "activity_description", "student_full_name"} {
		if value := payloadValue(payload, key); value != "" {
			return value
		}
	}
	return row.CategoryName
}

// buildAnnex1Sheets строит Приложение № 1 к Отчёту: по листу на каждую ОО или
// РОИВ, строка на мероприятие и итоговая строка. Суммы — в тыс. руб., как в
// форме; пустые строки не выгружаются.
func buildAnnex1Sheets(data []regulatoryRow) []annex1Sheet {
	order := []string{}
	byPartner := map[string][]regulatoryRow{}
	for _, row := range data {
		if row.Amount == 0 {
			continue
		}
		name := row.Partner
		if strings.TrimSpace(name) == "" {
			name = "Без контрагента"
		}
		if _, seen := byPartner[name]; !seen {
			order = append(order, name)
		}
		byPartner[name] = append(byPartner[name], row)
	}
	sort.Strings(order)
	sheets := make([]annex1Sheet, 0, len(order))
	for _, name := range order {
		rows := [][]interface{}{}
		var total money.Amount
		for index, row := range byPartner[name] {
			payload := map[string]interface{}{}
			_ = json.Unmarshal(row.Payload, &payload)
			volume, _, unit := activityMetrics(row.Category, numericPayload(row.Payload))
			// Стоимость единицы восстанавливается из суммы и объёма: тариф
			// хранится в расчётах, а не в записи мероприятия.
			var unitCost interface{} = "—"
			if volume > 0 {
				unitCost = math.Round(float64(row.Amount)/volume/1000) / 100
			}
			total, _ = money.Add(total, row.Amount)
			rows = append(rows, []interface{}{
				index + 1, row.CategoryName, activityTitle(row, payload), unit,
				annex1Indicator[row.Category], volume,
				unitCost, thousandRub(row.Amount), readablePayload(row.Category, row.Payload),
			})
		}
		rows = append(rows, []interface{}{"", "ИТОГО", "", "", "", "", "", thousandRub(total), ""})
		rows = appendSignatureBlock(rows, len(regulatoryHeaders["annex1"]))
		sheets = append(sheets, annex1Sheet{Name: name, Rows: rows})
	}
	return sheets
}

// payloadValue возвращает текстовое значение поля payload мероприятия или
// пустую строку, если поле отсутствует. Голый fmt.Sprint(p[key]) на
// отсутствующем ключе печатает буквальное "<nil>" в ячейку регламентной
// формы — это и есть источник дефекта, а не отсутствие данных мероприятия.
func payloadValue(p map[string]interface{}, key string) string {
	v, ok := p[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// internshipProgramName — графа «Наименование программы стажировок»: форма
// сводит стажёров в программу, поэтому именем служат реквизиты договора о
// стажировке или практической подготовке, а при их отсутствии —
// специальность по Приказу № 27.
func internshipProgramName(row regulatoryRow, payload map[string]interface{}) string {
	if row.Category == "employment_practice" {
		if number := payloadValue(payload, "practice_agreement_number"); number != "" {
			if date := payloadValue(payload, "practice_agreement_date"); date != "" {
				return fmt.Sprintf("Договор практической подготовки № %s от %s", number, formatRuDate(date))
			}
			return fmt.Sprintf("Договор практической подготовки № %s", number)
		}
	}
	for _, key := range []string{"internship_agreement_reference", "practice_agreement_reference", "individual_program_reference", "specialty_code"} {
		if value := payloadValue(payload, key); value != "" {
			return value
		}
	}
	return row.CategoryName
}

// buildAnnex2Rows строит Приложение № 2 к Отчёту: строка на программу
// стажировок с суммарными часами и затратами в тыс. руб., как в форме
// Приказа, а не построчный список стажёров.
func buildAnnex2Rows(data []regulatoryRow) [][]interface{} {
	type program struct {
		partner, name string
		students      map[string]bool
		hours         float64
		amount        money.Amount
	}
	order := []string{}
	programs := map[string]*program{}
	for _, row := range data {
		if row.Amount == 0 {
			continue
		}
		payload := map[string]interface{}{}
		_ = json.Unmarshal(row.Payload, &payload)
		name := internshipProgramName(row, payload)
		key := row.PartnerID + "\x00" + name
		item := programs[key]
		if item == nil {
			item = &program{partner: row.Partner, name: name, students: map[string]bool{}}
			programs[key] = item
			order = append(order, key)
		}
		if student := payloadValue(payload, "student_full_name"); student != "" {
			item.students[student] = true
		}
		hours, _, _ := activityMetrics(row.Category, numericPayload(row.Payload))
		item.hours += hours
		item.amount, _ = money.Add(item.amount, row.Amount)
	}
	sort.Strings(order)
	out := [][]interface{}{}
	var total money.Amount
	for index, key := range order {
		item := programs[key]
		var unitCost interface{} = "—"
		if item.hours > 0 {
			unitCost = math.Round(float64(item.amount)/item.hours/1000) / 100
		}
		total, _ = money.Add(total, item.amount)
		out = append(out, []interface{}{
			index + 1, item.name, "астрономический час", "Часы стажировки", item.hours,
			unitCost, thousandRub(item.amount),
			fmt.Sprintf("%s; стажёров: %d", item.partner, len(item.students)),
		})
	}
	if len(out) > 0 {
		out = append(out, []interface{}{"", "ИТОГО", "", "", "", "", thousandRub(total), ""})
		out = appendSignatureBlock(out, len(regulatoryHeaders["annex2"]))
	}
	return out
}

// buildMentorRows строит специализированный «Отчёт по наставникам»: сколько
// стажёров закреплено за наставником, сколько часов сопровождения
// подтверждено и на какую сумму. Ставка наставника по Приказу начисляется
// за каждого закреплённого стажёра персонально, поэтому строки суммируются
// по наставнику, а не по стажёру.
func buildMentorRows(data []regulatoryRow) [][]interface{} {
	type mentor struct {
		name, partner string
		students      map[string]bool
		hours         float64
		amount        money.Amount
	}
	order := []string{}
	mentors := map[string]*mentor{}
	for _, row := range data {
		payload := map[string]interface{}{}
		_ = json.Unmarshal(row.Payload, &payload)
		name := payloadValue(payload, "mentor_full_name")
		if name == "" {
			continue // без наставника строка в отчёт по наставникам не попадает
		}
		key := row.PartnerID + "\x00" + name
		item := mentors[key]
		if item == nil {
			item = &mentor{name: name, partner: row.Partner, students: map[string]bool{}}
			mentors[key] = item
			order = append(order, key)
		}
		if student := payloadValue(payload, "student_full_name"); student != "" {
			item.students[student] = true
		}
		item.hours += mentorHours(numericPayload(row.Payload))
		item.amount, _ = money.Add(item.amount, row.Amount)
	}
	sort.Strings(order)
	out := [][]interface{}{}
	var totalHours float64
	var totalAmount money.Amount
	totalStudents := 0
	for index, key := range order {
		item := mentors[key]
		totalHours += item.hours
		totalStudents += len(item.students)
		totalAmount, _ = money.Add(totalAmount, item.amount)
		out = append(out, []interface{}{index + 1, item.name, item.partner, len(item.students), item.hours, thousandRub(item.amount)})
	}
	// INT-09: срез по наставникам закрывается итогом, который должен
	// сходиться с суммой тех же мероприятий в других формах.
	if len(out) > 0 {
		out = append(out, []interface{}{"", "ИТОГО", "", totalStudents, totalHours, thousandRub(totalAmount)})
		out = appendSignatureBlock(out, len(regulatoryHeaders["annex2_mentors"]))
	}
	return out
}

// buildPlanFactRows сводит план/факт по контрагенту и виду мероприятия для
// конструктора срезов (REPORT-10). Пустые строки (план=0 и факт=0)
// программно удаляются независимо от настроек фильтрации в UI.
func buildPlanFactRows(data []regulatoryRow) [][]interface{} {
	type agg struct {
		name, category string
		plan, fact     money.Amount
	}
	totals := map[string]*agg{}
	for _, row := range data {
		key := row.PartnerID + "\x00" + row.Category
		if totals[key] == nil {
			totals[key] = &agg{name: row.Partner, category: row.CategoryName}
		}
		if row.Period == "plan" {
			totals[key].plan, _ = money.Add(totals[key].plan, row.Amount)
		} else {
			totals[key].fact, _ = money.Add(totals[key].fact, row.Amount)
		}
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := [][]interface{}{}
	for _, key := range keys {
		a := totals[key]
		if a.plan == 0 && a.fact == 0 {
			continue
		}
		delta := float64(a.fact-a.plan) / 100
		var percent interface{} = "—"
		if a.plan > 0 {
			percent = math.Round(float64(a.fact-a.plan)/float64(a.plan)*100*100) / 100
		}
		out = append(out, []interface{}{a.name, a.category, a.plan, a.fact, delta, percent})
	}
	return out
}

func buildPlanFactProjectionRows(groups []activityprojection.Aggregate, partnerNames, categoryNames map[string]string) [][]interface{} {
	out := make([][]interface{}, 0, len(groups))
	for _, group := range groups {
		if group.PlanAmount == 0 && group.FactAmount == 0 {
			continue
		}
		delta := float64(group.FactAmount-group.PlanAmount) / 100
		var percent interface{} = "—"
		if group.PlanAmount > 0 {
			percent = math.Round(float64(group.FactAmount-group.PlanAmount)/float64(group.PlanAmount)*100*100) / 100
		}
		partnerName := partnerNames[group.PartnerID]
		categoryName := categoryNames[group.CategoryCode]
		if categoryName == "" {
			categoryName = group.CategoryCode
		}
		out = append(out, []interface{}{partnerName, categoryName, group.PlanAmount, group.FactAmount, delta, percent})
	}
	return out
}
