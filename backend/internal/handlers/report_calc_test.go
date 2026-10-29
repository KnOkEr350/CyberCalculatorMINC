package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"cybercalc/internal/xlsx"
)

func calcTestRows() []regulatoryRow {
	payload := func(v map[string]interface{}) []byte {
		raw, _ := json.Marshal(v)
		return raw
	}
	internship := map[string]interface{}{"student_full_name": "Иванов", "mentor_full_name": "Петров Пётр", "internship_agreement_reference": "Договор 1",
		"student_load_hours_per_month": 80, "mentor_load_hours_per_month": 10, "duration_months": 2}
	return []regulatoryRow{
		{PartnerID: "p1", Partner: "МФТИ", Category: "internship", CategoryName: "Стажировки", Period: "fact", Audience: "vuz",
			Amount: 17580000, FormulaAmount: 17580000, CostMethod: "average", Payload: payload(internship)},
		{PartnerID: "p1", Partner: "МФТИ", Category: "internship", CategoryName: "Стажировки", Period: "plan", Audience: "vuz",
			Amount: 17580000, FormulaAmount: 17580000, CostMethod: "average", Payload: payload(internship)},
		{PartnerID: "p2", Partner: "ВШЭ", Category: "teachers", CategoryName: "Преподаватели", Period: "fact", Audience: "vuz",
			Amount: 29808000, FormulaAmount: 29808000, CostMethod: "average", Payload: payload(map[string]interface{}{"academic_hours": 72, "course_name": "Алгоритмы"})},
		{PartnerID: "p2", Partner: "ВШЭ", Category: "teachers", CategoryName: "Преподаватели", Period: "fact", Audience: "vuz",
			Amount: 1234567, FormulaAmount: 29808000, CostMethod: "actual", Payload: payload(map[string]interface{}{"academic_hours": 72, "course_name": "Базы данных"})},
	}
}

func cellTexts(rows [][]interface{}) [][]string {
	out := make([][]string, len(rows))
	for i, row := range rows {
		for _, cell := range row {
			out[i] = append(out[i], xlsx.CellText(cell))
		}
	}
	return out
}

func cloneRows(rows [][]interface{}) [][]interface{} {
	out := make([][]interface{}, len(rows))
	for i, row := range rows {
		out[i] = append([]interface{}{}, row...)
	}
	return out
}

func TestFormulizedFormsKeepBuilderValues(t *testing.T) {
	data := calcTestRows()
	check := func(name string, want, got [][]interface{}) {
		t.Helper()
		a, b := cellTexts(want), cellTexts(got)
		formulas := 0
		for i := range a {
			for j := range a[i] {
				if j < len(b[i]) && a[i][j] != b[i][j] {
					t.Fatalf("%s: cell %d/%d changed: %q -> %q", name, i, j, a[i][j], b[i][j])
				}
			}
			for _, cell := range got[i] {
				switch cell.(type) {
				case xlsx.Formula, xlsx.NumberFormula:
					formulas++
				}
			}
		}
		if formulas == 0 {
			t.Fatalf("%s: no formulas", name)
		}
	}
	annex2 := buildAnnex2Rows(data)
	got := cloneRows(annex2)
	formulizeAnnex2(got, data, newCalcBook(tariffs.Default()))
	check("annex2", annex2, got)

	mentors := buildMentorRows(data)
	got = cloneRows(mentors)
	formulizeMentors(got, data, newCalcBook(tariffs.Default()))
	check("mentors", mentors, got)

	annex5, _, _ := buildAnnex5Rows(data, money.Amount(100000000))
	got = cloneRows(annex5)
	formulizeAnnex5(got, data, newCalcBook(tariffs.Default()), 2026, money.Amount(100000000), true)
	check("annex5", annex5, got)

	planFact := buildPlanFactRows(data)
	got = formulizePlanFact(cloneRows(planFact), data, newCalcBook(tariffs.Default()))
	check("plan_fact", planFact, got[:len(planFact)])

	sheets := buildAnnex1Sheets(data)
	originals := make([][][]interface{}, len(sheets))
	for i := range sheets {
		originals[i] = cloneRows(sheets[i].Rows)
	}
	formulizeAnnex1(sheets, data, newCalcBook(tariffs.Default()))
	for i := range sheets {
		check(fmt.Sprintf("annex1 %d", i), originals[i], sheets[i].Rows)
	}
}

func TestCalcBookFormulasAndFallbacks(t *testing.T) {
	book := newCalcBook(tariffs.Default())
	data := calcTestRows()
	average := book.add(calcEntryFromRegulatory(data[2], 1))
	actual := book.add(calcEntryFromRegulatory(data[3], 1))
	row := book.rows[average-2]
	method, ok := row[calcColMethod].(xlsx.Formula)
	if !ok || method.Expr != fmt.Sprintf("ROUND(K%d*M%d,2)", average, average) || method.Value != 29808000 {
		t.Fatalf("average method cell: %#v", row[calcColMethod])
	}
	rate, ok := row[12].(xlsx.Formula)
	if !ok || !strings.HasPrefix(rate.Expr, "'Ставки приказа'!$C$") || rate.Value != 414000 {
		t.Fatalf("rate cell: %#v", row[12])
	}
	if amount, ok := row[calcColAmount].(xlsx.Formula); !ok || amount.Expr != fmt.Sprintf("T%d", average) {
		t.Fatalf("report amount should reference method: %#v", row[calcColAmount])
	}
	if value, ok := book.rows[actual-2][calcColAmount].(money.Amount); !ok || value != 1234567 {
		t.Fatalf("actual cost must stay a value: %#v", book.rows[actual-2][calcColAmount])
	}
	expr, total := book.sumForm(1, "fact")
	if total != 29808000+1234567 || !strings.Contains(expr, `"Факт"`) {
		t.Fatalf("sumForm %s %s", expr, total)
	}

	stale := data[2]
	stale.FormulaAmount, stale.Amount = 1000, 1000
	staleRow := book.rows[book.add(calcEntryFromRegulatory(stale, 0))-2]
	if value, ok := staleRow[calcColMethod].(money.Amount); !ok || value != 1000 || !strings.Contains(fmt.Sprint(staleRow[8]), "другой редакции") {
		t.Fatalf("stale rate edition must keep stored amount: %#v / %v", staleRow[calcColMethod], staleRow[8])
	}
}
