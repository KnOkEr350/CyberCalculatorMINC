package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"cybercalc/internal/docx"
	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"cybercalc/internal/xlsx"
)

type regulatoryRow struct {
	PartnerID, Partner, AgreementID, Agreement, Category, CategoryName, Period, Audience string
	Amount                                                                               money.Amount
	Payload                                                                              []byte
}

var regulatoryHeaders = map[string][]string{
	"annex1": {"№", "ОО / РОИВ", "Соглашение", "Вид мероприятия", "Уровень", "Срез", "Показатели и документы", "Сумма, руб."},
	"annex2": {"№", "ОО", "Студент", "Наставник", "Месяцев", "Часы студента", "Часы наставника", "Срочный ТД", "Сумма, руб."},
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
	if company == "" {
		middleware.WriteError(w, 400, "для отчёта назначьте ИТ-компанию")
		return
	}
	if kind == "agreement2" || kind == "agreement3" {
		h.exportAgreementTemplate(w, r, u, year, kind, company, partner, agreement)
		return
	}
	if kind == "annex3" && (partner == "" || agreement == "") {
		// Приложение № 3 — справка по конкретному соглашению; без реквизитов
		// соглашения формулировка «Соглашение с … от … №…» невозможна.
		middleware.WriteError(w, 400, "выберите партнёра и соглашение")
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
	if kind == "annex3" {
		conds = append(conds, "e.period_type='fact'")
	}
	// conds contains only fixed SQL fragments and $N placeholders; request values are in args.
	// nosemgrep: go.lang.security.injection.tainted-sql-string.tainted-sql-string
	query := `SELECT COALESCE(e.partner_id::text,''),COALESCE(p.name,''),COALESCE(e.agreement_id::text,''),COALESCE(a.number,''),e.category_code,c.name,e.period_type,e.audience,e.amount_rub,e.payload
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
		if rows.Scan(&item.PartnerID, &item.Partner, &item.AgreementID, &item.Agreement, &item.Category, &item.CategoryName, &item.Period, &item.Audience, &item.Amount, &item.Payload) != nil {
			middleware.WriteError(w, 500, "не удалось прочитать отчёт")
			return
		}
		data = append(data, item)
	}
	if kind == "annex3" {
		if len(data) > 0 {
			middleware.WriteError(w, 409, "справка об отсутствии недоступна: по соглашению есть мероприятия")
			return
		}
		var number, signedOn, partnerName string
		err := h.DB.QueryRowContext(r.Context(), `SELECT a.number,a.signed_on::text,p.name
			FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id JOIN partners p ON p.id=ap.partner_id
			WHERE a.id::text=$1 AND p.id::text=$2 AND a.it_company_id::text=$3`, agreement, partner, company).Scan(&number, &signedOn, &partnerName)
		if err != nil {
			middleware.WriteError(w, 404, "соглашение не найдено")
			return
		}
		statement := formatAbsenceStatement(partnerName, signedOn, number)
		body, err := docx.Table(
			fmt.Sprintf("Приложение № 3. Информационная справка об отсутствии реализованных мероприятий за %d год", year),
			[]string{"Соглашение", "Основание"},
			[][]string{{statement, "Фактически подтверждённые мероприятия отсутствуют"}},
		)
		if err != nil {
			middleware.WriteError(w, 500, "не удалось сформировать справку")
			return
		}
		h.writeGenerated(w, r, u, kind, "docx", fmt.Sprintf("приложение_3_%d.docx", year), company, reportFilters("partner_id", partner, "agreement_id", agreement), year, body)
		return
	}
	if len(data) == 0 {
		middleware.WriteError(w, 409, "нет утверждённых данных для формы")
		return
	}
	wb := xlsx.New()
	filename := ""
	switch kind {
	case "annex1":
		out := [][]interface{}{}
		for i, row := range data {
			out = append(out, []interface{}{i + 1, row.Partner, row.Agreement, row.CategoryName, officeValue(row.Audience), officeValue(row.Period), readablePayload(row.Category, row.Payload), row.Amount})
		}
		wb.AddSheet("Приложение № 1", regulatoryHeaders["annex1"], out)
		filename = fmt.Sprintf("приложение_1_%d.xlsx", year)
	case "annex2":
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
	h.writeGenerated(w, r, u, kind, "xlsx", filename, company, reportFilters("partner_id", partner, "agreement_id", agreement), year, body)
}

func (h *ReportHandlers) exportAgreementTemplate(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, kind, company, partner, agreement string) {
	if agreement == "" || partner == "" {
		middleware.WriteError(w, 400, "выберите партнёра и соглашение")
		return
	}
	var number, signed, partnerName, partnerINN, companyName, companyINN, companyOGRN, address, director string
	err := h.DB.QueryRowContext(r.Context(), `SELECT a.number,a.signed_on::text,p.name,COALESCE(d.inn,''),c.name,c.inn,c.ogrn,c.legal_address,c.director_name FROM agreements a JOIN agreement_partners ap ON ap.agreement_id=a.id JOIN partners p ON p.id=ap.partner_id LEFT JOIN education_directory d ON d.id=p.directory_id JOIN accredited_it_companies c ON c.id=a.it_company_id WHERE a.id::text=$1 AND p.id::text=$2 AND c.id::text=$3`, agreement, partner, company).Scan(&number, &signed, &partnerName, &partnerINN, &companyName, &companyINN, &companyOGRN, &address, &director)
	if err != nil {
		middleware.WriteError(w, 404, "соглашение не найдено")
		return
	}
	title := map[string]string{"agreement2": "Типовое соглашение — Приложение № 2", "agreement3": "Типовое соглашение — Приложение № 3"}[kind]
	body, err := docx.Table(title, []string{"Реквизит", "Значение"}, [][]string{{"Номер и дата", number + " от " + signed}, {"ИТ-организация", companyName}, {"ИНН / ОГРН", companyINN + " / " + companyOGRN}, {"Адрес", address}, {"Подписант", director}, {"Контрагент", partnerName}, {"ИНН контрагента", partnerINN}, {"Отчётный год", strconv.Itoa(year)}})
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать типовое соглашение")
		return
	}
	h.writeGenerated(w, r, u, kind, "docx", fmt.Sprintf("типовое_соглашение_%s_%d.docx", strings.TrimPrefix(kind, "agreement"), year), company, reportFilters("partner_id", partner, "agreement_id", agreement), year, body)
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
	targetThousandRub := float64(target) / 100000
	number := 0
	for _, key := range keys {
		a := totals[key]
		if a.fact == 0 {
			continue
		}
		var percent interface{} = "—"
		if target > 0 {
			percent = float64(a.fact) / float64(target) * 100
		}
		number++
		out = append(out, []interface{}{
			number,
			names[key],
			strings.Join(a.agreements, "; "),
			targetThousandRub,
			float64(a.fact) / 100000,
			percent,
		})
	}
	return out, planTotal, factTotal
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

// buildAnnex2Rows строит реестр стажировок для Приложения № 2 к Отчёту:
// студент, наставник, срок и реквизиты срочного ТД по каждой строке
// подтверждённых мероприятий вида «Стажировка/Практика».
func buildAnnex2Rows(data []regulatoryRow) [][]interface{} {
	out := [][]interface{}{}
	for i, row := range data {
		var p map[string]interface{}
		_ = json.Unmarshal(row.Payload, &p)
		out = append(out, []interface{}{
			i + 1, row.Partner,
			payloadValue(p, "student_full_name"), payloadValue(p, "mentor_full_name"),
			payloadValue(p, "duration_months"), payloadValue(p, "total_student_hours"), payloadValue(p, "total_mentor_hours"),
			payloadValue(p, "labor_contract_number"), row.Amount,
		})
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
		percent := 0.0
		if a.plan > 0 {
			percent = float64(a.fact-a.plan) / float64(a.plan) * 100
		}
		out = append(out, []interface{}{a.name, a.category, a.plan, a.fact, delta, percent})
	}
	return out
}
