package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/xlsx"
)

type ReportHandlers struct {
	DB *sql.DB
}

type reportEntryRow struct {
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
	if year == 0 {
		middleware.WriteError(w, http.StatusBadRequest, "укажите report_year")
		return
	}
	periodType := q.Get("period_type")
	if periodType != "plan" && periodType != "fact" {
		middleware.WriteError(w, http.StatusBadRequest, "period_type должен быть plan или fact")
		return
	}
	categoryFilter := q.Get("category_code") // пусто = все категории (годовой план); можно ограничить, напр. internship

	conditions := []string{"e.period_type = $1", "e.report_year = $2"}
	args := []interface{}{periodType, year}
	if categoryFilter != "" {
		args = append(args, categoryFilter)
		conditions = append(conditions, "e.category_code = $"+strconv.Itoa(len(args)))
	}
	conditions, args = appendEntryScope(conditions, args, u, "e")
	query := `SELECT p.name, e.category_code, e.audience, e.amount_rub, e.payload
		FROM entries e LEFT JOIN partners p ON p.id = e.partner_id
		WHERE ` + strings.Join(conditions, " AND ")
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
		if err := rows.Scan(&row.PartnerName, &row.CategoryCode, &row.Audience, &row.AmountRub, &row.Payload); err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "ошибка чтения")
			return
		}
		data = append(data, row)
	}

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
		consolidated = append(consolidated, []interface{}{partnerName, d.CategoryCode, d.Audience, d.AmountRub, payloadSummary(d.Payload)})
		total += d.AmountRub
	}
	consolidated = append(consolidated, []interface{}{"ИТОГО", "", "", total, ""})
	wb.AddSheet("Сводный для МЦ", headers, consolidated)

	// Отдельный лист по каждому партнёру.
	byPartner := map[string][][]interface{}{}
	var order []string
	for _, d := range data {
		partnerName := "Без партнёра"
		if d.PartnerName.Valid {
			partnerName = d.PartnerName.String
		}
		if _, ok := byPartner[partnerName]; !ok {
			order = append(order, partnerName)
		}
		byPartner[partnerName] = append(byPartner[partnerName], []interface{}{partnerName, d.CategoryCode, d.Audience, d.AmountRub, payloadSummary(d.Payload)})
	}
	for _, name := range order {
		wb.AddSheet(name, headers, byPartner[name])
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
	for k, v := range m {
		if s != "" {
			s += "; "
		}
		s += fmt.Sprintf("%s=%v", k, v)
	}
	return s
}
