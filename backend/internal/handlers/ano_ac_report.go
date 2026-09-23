package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"cybercalc/internal/middleware"
	"cybercalc/internal/money"
	"cybercalc/internal/topit"
	"cybercalc/internal/xlsx"
)

var anoRoleLabels = map[string]string{"anchor": "Якорный", "partner": "Индустриальный"}

// exportAnoAc (REPORT-09) собирает отчёт по одной программе ТОП-ИТ/ТОП-ИИ в
// шесть листов по ТЗ 4.4 §9.3: паспорт, денежное софинансирование, неденежная
// поддержка, стипендиаты, кейсы, РИД. Структура взята из описания ТЗ:
// официальная форма АНО АЦ в реестре BASE-13 не зарегистрирована, поэтому на
// листе паспорта это отмечено. Числа не пересчитываются заново: шкала
// софинансирования — та же, что в карточке программы (ADR-14).
func (h *ReportHandlers) exportAnoAc(w http.ResponseWriter, r *http.Request, u middleware.AuthUser, year int, company string) {
	entryID := strings.TrimSpace(r.URL.Query().Get("entry_id"))
	if entryID == "" {
		middleware.WriteError(w, 400, "укажите entry_id — запись программы ТОП-ИТ/ТОП-ИИ")
		return
	}
	var partnerName, project, program, wave, role, grant, planned string
	var transferred, spent, article, agreementRef, paymentRef, actRef string
	err := h.DB.QueryRowContext(r.Context(), `SELECT COALESCE(p.name,''),COALESCE(e.top_project_name,''),COALESCE(e.top_program_name,''),
			COALESCE(e.top_wave,''),COALESCE(e.top_partner_role,''),COALESCE(e.top_grant_rub::text,''),COALESCE(e.top_planned_cofinancing_rub::text,''),
			COALESCE(NULLIF(e.payload->>'transferred_amount_rub',''),''),COALESCE(NULLIF(e.payload->>'actual_spent_amount_rub',''),''),
			COALESCE(e.payload->>'expense_article',''),COALESCE(e.payload->>'top_agreement_reference',''),
			COALESCE(e.payload->>'payment_order_reference',''),COALESCE(e.payload->>'spending_act_reference','')
		FROM entries e LEFT JOIN partners p ON p.id=e.partner_id
		WHERE e.id::text=$1 AND e.it_company_id::text=$2 AND e.category_code='top_it' AND e.report_year=$3`, entryID, company, year).
		Scan(&partnerName, &project, &program, &wave, &role, &grant, &planned, &transferred, &spent, &article, &agreementRef, &paymentRef, &actRef)
	if err != nil {
		middleware.WriteError(w, 404, "программа ТОП-ИТ/ТОП-ИИ за этот год не найдена")
		return
	}
	items, err := topit.List(r.Context(), h.DB, entryID)
	if err != nil {
		middleware.WriteError(w, 500, "не удалось прочитать составляющие программы")
		return
	}
	amount := func(raw string) money.Amount {
		value, err := money.Parse(raw)
		if err != nil {
			return 0
		}
		return value
	}
	optional := func(value *money.Amount) interface{} {
		if value == nil {
			return ""
		}
		return *value
	}
	share := func(value *float64) interface{} {
		if value == nil {
			return ""
		}
		return *value
	}
	scale := topit.Scale(amount(planned), amount(spent))
	scaleText := map[string]string{"green": "зелёная (≥100%)", "yellow": "жёлтая (70–99,9%)", "red": "красная (<70%)", "unknown": "не определена: не задан план"}[scale.State]

	passport := [][]interface{}{
		{"Вуз", partnerName}, {"Программа", program}, {"Федеральный проект", project}, {"Волна", wave},
		{"Статус вуза", anoRoleLabels[role]}, {"Размер гранта, руб.", amount(grant)},
		{"Норма вуза, руб. (" + fmt.Sprint(topit.UniversityNormPercent) + "% от гранта)", topit.UniversityNorm(amount(grant))},
		{"План софинансирования X, руб.", amount(planned)}, {"Отчётный год", year},
		{"Примечание", "Структура листов по ТЗ 4.4 §9.3; официальная форма АНО АЦ в реестре нормативных источников не зарегистрирована."},
	}
	cash := [][]interface{}{
		{"План софинансирования X", amount(planned)}, {"Перечислено", amount(transferred)}, {"Фактически списано вузом (в зачёт)", amount(spent)},
		{"Ход софинансирования, %", scale.Percent}, {"Шкала", scaleText}, {"Статья расходов", article},
		{"Договор", agreementRef}, {"Платёжное поручение", paymentRef}, {"Акт / отчёт о расходовании", actRef},
	}
	var support, scholarships, cases, rids [][]interface{}
	for _, item := range items {
		switch topit.Kind(item.Kind) {
		case topit.KindSupport:
			support = append(support, []interface{}{item.Title, map[string]string{"equipment": "Оборудование", "software": "Программное обеспечение"}[item.SupportKind],
				item.ActReference, item.ActDate, optional(item.BalanceValueRub), optional(item.AppraisedValueRub), optional(item.ConfirmedValueRub)})
		case topit.KindScholarship:
			scholarships = append(scholarships, []interface{}{item.StudentName, item.GroupName, item.Course, item.PeriodStart, item.PeriodEnd,
				optional(item.AmountRub), item.Criterion, item.DonorName})
		case topit.KindCase:
			cases = append(cases, []interface{}{item.Title, item.ImplementationOrg, map[string]string{"proposed": "Предложен", "implemented": "Внедрён"}[item.ImplementationStatus],
				item.ImplementedOn, item.Description})
		case topit.KindRID:
			rids = append(rids, []interface{}{item.Title, map[string]string{"software": "Программное обеспечение", "ai_model": "Модель ИИ", "dataset": "Набор данных"}[item.RIDType],
				item.Authors, share(item.UniversitySharePct), share(item.CompanySharePct)})
		}
	}
	workbook := xlsx.New()
	workbook.AddSheet("Паспорт", []string{"Показатель", "Значение"}, passport)
	workbook.AddSheet("Денежное софинансирование", []string{"Показатель", "Значение"}, cash)
	workbook.AddSheet("Неденежная поддержка", []string{"Название", "Вид", "Акт", "Дата акта", "Балансовая, руб.", "Оценочная, руб.", "Подтверждённая, руб."}, support)
	workbook.AddSheet("Стипендиаты", []string{"Студент", "Группа", "Курс", "Начало", "Конец", "Сумма, руб.", "Критерий", "Донор"}, scholarships)
	workbook.AddSheet("Кейсы", []string{"Название", "Организация", "Статус", "Дата внедрения", "Описание"}, cases)
	workbook.AddSheet("РИД", []string{"Название", "Тип", "Авторы", "Доля вуза, %", "Доля компании, %"}, rids)
	body, err := workbook.Bytes()
	if err != nil {
		middleware.WriteError(w, 500, "не удалось сформировать отчёт")
		return
	}
	h.writeGenerated(w, r, u, "ano_ac", "xlsx", fmt.Sprintf("отчёт_АНО_АЦ_%d.xlsx", year), company, reportFilters("entry_id", entryID), year, body)
}
