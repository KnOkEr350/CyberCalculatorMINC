package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"cybercalc/internal/money"
	"cybercalc/internal/xlsx"
)

// Формы Приказа строятся построителями значений (они же дают CSV и
// проверяются тестами). Для Excel суммы этих форм заменяются формулами с
// тем же кэшированным значением: каждая сумма ссылается на лист «Расчёт
// сумм», где сумма мероприятия посчитана от показателей и ставок Приказа.
// Группировка ниже повторяет группировку соответствующего построителя.

func calcEntryFromRegulatory(row regulatoryRow, formRow int) calcEntry {
	payload := map[string]interface{}{}
	_ = json.Unmarshal(row.Payload, &payload)
	return calcEntry{
		Partner: row.Partner, CategoryCode: row.Category, CategoryName: row.CategoryName, Title: activityTitle(row, payload),
		Audience: row.Audience, Period: row.Period, CostMethod: row.CostMethod, Payload: payload,
		Amount: row.Amount, FormulaAmount: row.FormulaAmount, FormRow: formRow,
	}
}

func thousandFormula(rubExpr string, amount money.Amount) xlsx.NumberFormula {
	return xlsx.NumberFormula{Expr: fmt.Sprintf("ROUND(%s/1000,2)", rubExpr), Value: thousandRub(amount)}
}

func unitCostFormula(rubExpr string, volumeRef string, amount money.Amount, volume float64) interface{} {
	if volume <= 0 {
		return "-"
	}
	return xlsx.NumberFormula{Expr: fmt.Sprintf("ROUND(%s/%s/1000,2)", rubExpr, volumeRef), Value: math.Round(float64(amount)/volume/1000) / 100}
}

func allFormsExpr(period string) string {
	expr := fmt.Sprintf(`SUMIFS(%s,%s,">0"`, xlsx.SheetColumn(calcSheetName, calcColAmount), xlsx.SheetColumn(calcSheetName, calcColFormRow))
	if period != "" {
		expr += fmt.Sprintf(`,%s,"%s"`, xlsx.SheetColumn(calcSheetName, calcColPeriod), calcPeriodLabels[period])
	}
	return expr + ")"
}

// formulizeAnnex1: лист на контрагента, строка на мероприятие. Группа
// формы - номер листа: по ней считается ИТОГО листа.
func formulizeAnnex1(sheets []annex1Sheet, data []regulatoryRow, book *calcBook) {
	byPartner := map[string][]regulatoryRow{}
	for _, row := range data {
		if row.Amount == 0 {
			continue
		}
		name := row.Partner
		if strings.TrimSpace(name) == "" {
			name = "Без контрагента"
		}
		byPartner[name] = append(byPartner[name], row)
	}
	for sheetIndex, sheet := range sheets {
		group := sheetIndex + 1
		entries := byPartner[sheet.Name]
		var total money.Amount
		for i, entry := range entries {
			calcRow := book.add(calcEntryFromRegulatory(entry, group))
			ref := book.amountRef(calcRow)
			volume, _ := sheet.Rows[i][5].(float64)
			sheet.Rows[i][6] = unitCostFormula(ref, xlsx.Ref(5, i+2), entry.Amount, volume)
			sheet.Rows[i][7] = thousandFormula(ref, entry.Amount)
			total, _ = money.Add(total, entry.Amount)
		}
		if len(entries) < len(sheet.Rows) {
			expr, _ := book.sumForm(group, "")
			sheet.Rows[len(entries)][7] = thousandFormula(expr, total)
		}
	}
}

// formulizeAnnex2: строка на программу стажировок.
func formulizeAnnex2(rows [][]interface{}, data []regulatoryRow, book *calcBook) {
	order := []string{}
	groups := map[string][]regulatoryRow{}
	for _, row := range data {
		if row.Amount == 0 {
			continue
		}
		payload := map[string]interface{}{}
		_ = json.Unmarshal(row.Payload, &payload)
		key := row.PartnerID + "\x00" + internshipProgramName(row, payload)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	sort.Strings(order)
	var total money.Amount
	for index, key := range order {
		group := index + 1
		for _, entry := range groups[key] {
			book.add(calcEntryFromRegulatory(entry, group))
		}
		expr, amount := book.sumForm(group, "")
		hours, _ := rows[index][4].(float64)
		rows[index][5] = unitCostFormula(expr, xlsx.Ref(4, index+2), amount, hours)
		rows[index][6] = thousandFormula(expr, amount)
		total, _ = money.Add(total, amount)
	}
	if len(order) > 0 && len(rows) > len(order) {
		rows[len(order)][6] = thousandFormula(allFormsExpr(""), total)
	}
}

// formulizeMentors: строка на наставника.
func formulizeMentors(rows [][]interface{}, data []regulatoryRow, book *calcBook) {
	order := []string{}
	groups := map[string][]regulatoryRow{}
	for _, row := range data {
		payload := map[string]interface{}{}
		_ = json.Unmarshal(row.Payload, &payload)
		name := payloadValue(payload, "mentor_full_name")
		if name == "" {
			continue
		}
		key := row.PartnerID + "\x00" + name
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	sort.Strings(order)
	var total money.Amount
	for index, key := range order {
		group := index + 1
		for _, entry := range groups[key] {
			book.add(calcEntryFromRegulatory(entry, group))
		}
		expr, amount := book.sumForm(group, "")
		rows[index][5] = thousandFormula(expr, amount)
		total, _ = money.Add(total, amount)
	}
	if len(order) > 0 && len(rows) > len(order) {
		last := len(order) + 1
		totalRow := rows[len(order)]
		if students, ok := totalRow[3].(int); ok {
			totalRow[3] = xlsx.NumberFormula{Expr: xlsx.SumFormula(3, 2, last), Value: float64(students)}
		}
		if hours, ok := totalRow[4].(float64); ok {
			totalRow[4] = xlsx.NumberFormula{Expr: xlsx.SumFormula(4, 2, last), Value: hours}
		}
		totalRow[5] = thousandFormula(allFormsExpr(""), total)
	}
}

// formulizeAnnex5: строка на контрагента с фактом; процент - от норматива
// на листе «Норматив 3%». Возвращает строку этого листа.
func formulizeAnnex5(rows [][]interface{}, data []regulatoryRow, book *calcBook, year int, target money.Amount, targetSet bool) []interface{} {
	keys := []string{}
	groups := map[string][]regulatoryRow{}
	fact := map[string]money.Amount{}
	for _, row := range data {
		if _, ok := groups[row.PartnerID]; !ok {
			keys = append(keys, row.PartnerID)
		}
		groups[row.PartnerID] = append(groups[row.PartnerID], row)
		if row.Period != "plan" {
			fact[row.PartnerID], _ = money.Add(fact[row.PartnerID], row.Amount)
		}
	}
	sort.Strings(keys)
	targetRef := "'Норматив 3%'!$B$2"
	percent := func(expr string, amount money.Amount) interface{} {
		if target <= 0 {
			return "-"
		}
		return xlsx.NumberFormula{Expr: fmt.Sprintf("ROUND(%s/%s*100,2)", expr, targetRef), Value: math.Round(float64(amount)/float64(target)*100*100) / 100}
	}
	targetCell := func() interface{} {
		if !targetSet {
			return thousandRub(target)
		}
		return thousandFormula(targetRef, target)
	}
	index := 0
	for _, key := range keys {
		if fact[key] == 0 {
			for _, entry := range groups[key] {
				book.add(calcEntryFromRegulatory(entry, 0))
			}
			continue
		}
		group := index + 1
		for _, entry := range groups[key] {
			book.add(calcEntryFromRegulatory(entry, group))
		}
		expr, amount := book.sumForm(group, "fact")
		rows[index][3] = targetCell()
		rows[index][4] = thousandFormula(expr, amount)
		rows[index][5] = percent(expr, amount)
		index++
	}
	planExpr, planTotal := book.sumPeriod("plan")
	factExpr, factTotal := book.sumPeriod("fact")
	if index > 0 && len(rows) > index {
		rows[index][3] = targetCell()
		rows[index][4] = thousandFormula(factExpr, factTotal)
		rows[index][5] = percent(factExpr, factTotal)
	}
	var targetValue interface{} = ""
	if targetSet {
		targetValue = target
	}
	return []interface{}{year, targetValue, xlsx.Formula{Expr: planExpr, Value: planTotal}, xlsx.Formula{Expr: factExpr, Value: factTotal}}
}

func planFactDeltaCells(rows [][]interface{}, index int) {
	row := rows[index]
	plan, factValue := cellAmount(row[2]), cellAmount(row[3])
	rowNum := index + 2
	row[4] = xlsx.NumberFormula{Expr: fmt.Sprintf("%s-%s", xlsx.Ref(3, rowNum), xlsx.Ref(2, rowNum)), Value: float64(factValue-plan) / 100}
	if plan > 0 {
		row[5] = xlsx.NumberFormula{Expr: fmt.Sprintf("ROUND((%s-%s)/%s*100,2)", xlsx.Ref(3, rowNum), xlsx.Ref(2, rowNum), xlsx.Ref(2, rowNum)), Value: math.Round(float64(factValue-plan)/float64(plan)*100*100) / 100}
	}
}

func planFactTotalRow(rows [][]interface{}) [][]interface{} {
	if len(rows) == 0 {
		return rows
	}
	var plan, factValue money.Amount
	for _, row := range rows {
		plan, _ = money.Add(plan, cellAmount(row[2]))
		factValue, _ = money.Add(factValue, cellAmount(row[3]))
	}
	last := len(rows) + 1
	total := []interface{}{"ИТОГО", "", xlsx.Formula{Expr: xlsx.SumFormula(2, 2, last), Value: plan}, xlsx.Formula{Expr: xlsx.SumFormula(3, 2, last), Value: factValue}, "", "-"}
	rows = append(rows, total)
	planFactDeltaCells(rows, len(rows)-1)
	return rows
}

// formulizePlanFact: строка на контрагента и вид мероприятия.
func formulizePlanFact(rows [][]interface{}, data []regulatoryRow, book *calcBook) [][]interface{} {
	keys := []string{}
	groups := map[string][]regulatoryRow{}
	for _, row := range data {
		key := row.PartnerID + "\x00" + row.Category
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], row)
	}
	sort.Strings(keys)
	index := 0
	for _, key := range keys {
		var plan, factValue money.Amount
		for _, entry := range groups[key] {
			if entry.Period == "plan" {
				plan, _ = money.Add(plan, entry.Amount)
			} else {
				factValue, _ = money.Add(factValue, entry.Amount)
			}
		}
		group := 0
		if plan != 0 || factValue != 0 {
			group = index + 1
		}
		for _, entry := range groups[key] {
			book.add(calcEntryFromRegulatory(entry, group))
		}
		if group == 0 {
			continue
		}
		planExpr, planAmount := book.sumForm(group, "plan")
		factExpr, factAmount := book.sumForm(group, "fact")
		rows[index][2] = xlsx.Formula{Expr: planExpr, Value: planAmount}
		rows[index][3] = xlsx.Formula{Expr: factExpr, Value: factAmount}
		planFactDeltaCells(rows, index)
		index++
	}
	return planFactTotalRow(rows)
}

// formulizePlanFactProjection: аналитическая проекция уже агрегирована, поэтому
// формулами считаются дельта, процент и итоги.
func formulizePlanFactProjection(rows [][]interface{}) [][]interface{} {
	for index := range rows {
		planFactDeltaCells(rows, index)
	}
	return planFactTotalRow(rows)
}
