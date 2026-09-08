package handlers

import (
	"cybercalc/internal/calculators"
	"cybercalc/internal/docx"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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
	PartnerID    string
	PartnerName  sql.NullString
	CategoryCode string
	Audience     string
	AmountRub    float64
	Payload      []byte
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
	periodType := q.Get("period_type")
	if periodType != "plan" && periodType != "fact" {
		middleware.WriteError(w, http.StatusBadRequest, "period_type должен быть plan или fact")
		return
	}
	categoryFilter := q.Get("category_code") // пусто = все категории (годовой план); можно ограничить, напр. internship

	query := `SELECT COALESCE(e.partner_id::text,''), p.name, e.category_code, e.audience, e.amount_rub, e.payload
		FROM entries e LEFT JOIN partners p ON p.id = e.partner_id
		WHERE e.period_type = $1 AND e.report_year = $2`
	args := []interface{}{periodType, year}
	if categoryFilter != "" {
		query += ` AND e.category_code = $3`
		args = append(args, categoryFilter)
	}
	if scope := partnerScope(u, q.Get("partner_id")); scope != "" {
		args = append(args, scope)
		query += fmt.Sprintf(" AND e.partner_id::text=$%d", len(args))
	}
	if mentor := q.Get("mentor_id"); mentor != "" {
		args = append(args, mentor)
		query += fmt.Sprintf(" AND e.payload->>'mentor_id'=$%d", len(args))
	}
	query += ` ORDER BY p.name NULLS LAST, e.category_code`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка запроса: "+err.Error())
		return
	}
	defer rows.Close()

	var data []reportEntryRow
	for rows.Next() {
		var row reportEntryRow
		if err := rows.Scan(&row.PartnerID, &row.PartnerName, &row.CategoryCode, &row.Audience, &row.AmountRub, &row.Payload); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		data = append(data, row)
	}
	if rows.Err() != nil {
		middleware.WriteError(w, 500, "ошибка чтения отчёта")
		return
	}
	categoryNames := map[string]string{}
	categoryRows, err := h.DB.Query(`SELECT code,name FROM activity_categories`)
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
	headers := []string{"Партнёр", "Категория активности", "Аудитория", "Сумма затрат, руб.", "Параметры"}

	// Сводный лист для МЦ — все партнёры вместе.
	var consolidated [][]interface{}
	var total float64
	for _, d := range data {
		partnerName := "—"
		if d.PartnerName.Valid {
			partnerName = d.PartnerName.String
		}
		consolidated = append(consolidated, []interface{}{partnerName, categoryNames[d.CategoryCode], audiences[d.Audience], d.AmountRub, readablePayload(d.CategoryCode, d.Payload)})
		total += d.AmountRub
	}
	consolidated = append(consolidated, []interface{}{"ИТОГО", "", "", total, ""})
	wb.AddSheet("Сводный для МЦ", headers, consolidated)
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
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report_%s_%d.docx"`, periodType, year))
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
			cols := []string{"Учебное заведение"}
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
				row := []interface{}{d.PartnerName.String}
				for _, f := range fields {
					v, ok := payload[f.Key]
					if !ok {
						v = ""
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
		byPartner[key] = append(byPartner[key], []interface{}{partnerName, categoryNames[d.CategoryCode], audiences[d.Audience], d.AmountRub, readablePayload(d.CategoryCode, d.Payload)})
	}
	for _, key := range order {
		wb.AddSheet(fmt.Sprint(byPartner[key][0][0]), headers, byPartner[key])
	}

	body, err := wb.Bytes()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "ошибка формирования файла")
		return
	}

	filename := fmt.Sprintf("report_%s_%d.xlsx", periodType, year)
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Write(body)

	logAudit(h.DB, "report", "", "export", u.ID, fmt.Sprintf("выгрузка %s", filename), nil, nil)
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
			parts = append(parts, f.Label+": "+fmt.Sprint(v))
		}
	}
	return strings.Join(parts, "; ")
}
