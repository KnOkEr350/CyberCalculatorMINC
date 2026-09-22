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
	"annex1":    {"№", "ОО / РОИВ", "Соглашение", "Вид мероприятия", "Уровень", "Срез", "Показатели и документы", "Сумма, руб."},
	"annex2":    {"№", "ОО", "Студент", "Наставник", "Месяцев", "Часы студента", "Часы наставника", "Срочный ТД", "Сумма, руб."},
	"annex5":    {"№", "Контрагент", "План, руб.", "Факт, руб.", "Дельта, руб.", "Дельта, %"},
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

func (h *ReportHandlers) writeGenerated(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, kind, format, filename, company, partner, agreement string, year int, body []byte) {
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	filters, _ := json.Marshal(map[string]string{"partner_id": partner, "agreement_id": agreement})
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO generated_reports(it_company_id,report_type,file_format,report_year,partner_id,agreement_id,file_name,content_sha256,size_bytes,content_bytes,filters,generated_by)
		VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10,$11,$12)`, company, kind, format, year, partner, agreement, filename, hash, len(body), body, filters, u.ID); err != nil {
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
	conds := []string{"e.it_company_id::text=$1", "e.report_year=$2", "eligibility.eligible"}
	args := []interface{}{company, year}
	if partner != "" {
		args = append(args, partner)
		conds = append(conds, fmt.Sprintf("e.partner_id::text=$%d", len(args)))
	}
	if agreement != "" {
		args = append(args, agreement)
		conds = append(conds, fmt.Sprintf("e.agreement_id::text=$%d", len(args)))
	}
	if kind == "annex2" {
		conds = append(conds, "e.category_code IN ('internship','employment_practice')")
	}
	if kind == "annex3" {
		conds = append(conds, "e.period_type='fact'")
	}
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
		body, _ := docx.Table(fmt.Sprintf("Приложение № 3. Информационная справка об отсутствии мероприятий за %d год", year), []string{"Соглашение", "Партнёр", "Основание"}, [][]string{{agreement, partner, "Фактически подтверждённые мероприятия отсутствуют"}})
		h.writeGenerated(w, r, u, kind, "docx", fmt.Sprintf("приложение_3_%d.docx", year), company, partner, agreement, year, body)
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
		out := [][]interface{}{}
		for i, row := range data {
			var p map[string]interface{}
			_ = json.Unmarshal(row.Payload, &p)
			out = append(out, []interface{}{i + 1, row.Partner, fmt.Sprint(p["student_full_name"]), fmt.Sprint(p["mentor_full_name"]), fmt.Sprint(p["duration_months"]), fmt.Sprint(p["total_student_hours"]), fmt.Sprint(p["total_mentor_hours"]), fmt.Sprint(p["labor_contract_number"]), row.Amount})
		}
		wb.AddSheet("Приложение № 2", regulatoryHeaders["annex2"], out)
		filename = fmt.Sprintf("приложение_2_%d.xlsx", year)
	case "annex5":
		type agg struct{ plan, fact money.Amount }
		totals := map[string]*agg{}
		names := map[string]string{}
		for _, row := range data {
			if totals[row.PartnerID] == nil {
				totals[row.PartnerID] = &agg{}
			}
			names[row.PartnerID] = row.Partner
			if row.Period == "plan" {
				totals[row.PartnerID].plan, _ = money.Add(totals[row.PartnerID].plan, row.Amount)
			} else {
				totals[row.PartnerID].fact, _ = money.Add(totals[row.PartnerID].fact, row.Amount)
			}
		}
		keys := make([]string, 0, len(totals))
		for key := range totals {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := [][]interface{}{}
		var planTotal, factTotal money.Amount
		for i, key := range keys {
			a := totals[key]
			delta := float64(a.fact-a.plan) / 100
			percent := 0.0
			if a.plan > 0 {
				percent = float64(a.fact-a.plan) / float64(a.plan) * 100
			}
			out = append(out, []interface{}{i + 1, names[key], a.plan, a.fact, delta, percent})
			planTotal, _ = money.Add(planTotal, a.plan)
			factTotal, _ = money.Add(factTotal, a.fact)
		}
		var target sql.NullString
		_ = h.DB.QueryRowContext(r.Context(), `SELECT target_amount_rub::text FROM organization_budget_targets WHERE it_company_id::text=$1 AND report_year=$2`, company, year).Scan(&target)
		wb.AddSheet("Приложение № 5", regulatoryHeaders["annex5"], out)
		wb.AddSheet("Норматив 3%", []string{"Год", "Норматив, руб.", "План, руб.", "Факт, руб."}, [][]interface{}{{year, target.String, planTotal, factTotal}})
		filename = fmt.Sprintf("приложение_5_%d.xlsx", year)
	case "plan_fact":
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
			delta := float64(a.fact-a.plan) / 100
			percent := 0.0
			if a.plan > 0 {
				percent = float64(a.fact-a.plan) / float64(a.plan) * 100
			}
			out = append(out, []interface{}{a.name, a.category, a.plan, a.fact, delta, percent})
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
	h.writeGenerated(w, r, u, kind, "xlsx", filename, company, partner, agreement, year, body)
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
	body, _ := docx.Table(title, []string{"Реквизит", "Значение"}, [][]string{{"Номер и дата", number + " от " + signed}, {"ИТ-организация", companyName}, {"ИНН / ОГРН", companyINN + " / " + companyOGRN}, {"Адрес", address}, {"Подписант", director}, {"Контрагент", partnerName}, {"ИНН контрагента", partnerINN}, {"Отчётный год", strconv.Itoa(year)}})
	h.writeGenerated(w, r, u, kind, "docx", fmt.Sprintf("типовое_соглашение_%s_%d.docx", strings.TrimPrefix(kind, "agreement"), year), company, partner, agreement, year, body)
}
