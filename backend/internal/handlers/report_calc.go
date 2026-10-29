package handlers

import (
	"fmt"
	"math/big"

	"cybercalc/internal/calculators"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"cybercalc/internal/tariffs"
	"cybercalc/internal/xlsx"
)

// Листы «Расчёт сумм» и «Ставки приказа» добавляются к каждой выгрузке с
// суммами мероприятий: сумма по методике записана формулой от показателей
// записи и ставок Приказа, а суммы форм ссылаются на неё. Так проверяющий
// видит, из чего сложилась каждая сумма, и может пересчитать её в Excel.
const (
	calcSheetName  = "Расчёт сумм"
	ratesSheetName = "Ставки приказа"
	calcInputPairs = 5

	calcColPeriod  = 5
	calcColFormRow = 6
	calcColMethod  = 19
	calcColAmount  = 20
)

var calcHeaders = func() []string {
	headers := []string{"№", "Учебное заведение", "Вид мероприятия", "Мероприятие", "Аудитория", "План / факт", "Строка формы", "Метод стоимости", "Расчёт по методике"}
	for i := 1; i <= calcInputPairs; i++ {
		headers = append(headers, fmt.Sprintf("Показатель %d", i), fmt.Sprintf("Значение %d", i))
	}
	return append(headers, "Сумма по методике, руб.", "Сумма в отчёте, руб.")
}()

var calcPeriodLabels = map[string]string{"plan": "План", "fact": "Факт"}

type calcEntry struct {
	Partner, CategoryCode, CategoryName, Title, Audience, Period, CostMethod string
	Payload                                                                  map[string]interface{}
	Amount                                                                   money.Amount
	FormulaAmount                                                            money.Amount
	FormRow                                                                  int
}

type calcBook struct {
	card    tariffs.Card
	rows    [][]interface{}
	amounts []money.Amount
	periods []string
	forms   []int
	rateRow map[string]int
}

func newCalcBook(card tariffs.Card) *calcBook {
	book := &calcBook{card: card, rateRow: map[string]int{}}
	for index, rate := range card.Rates() {
		book.rateRow[rate.Code] = index + 2
	}
	return book
}

func ratFloat(value *big.Rat) float64 {
	f, _ := value.Float64()
	return f
}

// add записывает мероприятие в «Расчёт сумм» и возвращает номер строки Excel.
func (b *calcBook) add(e calcEntry) int {
	rowNum := len(b.rows) + 2
	audience := map[string]string{"vuz": "Вуз", "kolledj": "СПО", "school": "Школа"}[e.Audience]
	row := make([]interface{}, len(calcHeaders))
	for i := range row {
		row[i] = ""
	}
	row[0], row[1], row[2], row[3], row[4] = len(b.rows)+1, e.Partner, e.CategoryName, e.Title, audience
	row[calcColPeriod] = calcPeriodLabels[e.Period]
	if e.FormRow > 0 {
		row[calcColFormRow] = e.FormRow
	}
	formulaAmount := e.FormulaAmount
	if formulaAmount == 0 && e.CostMethod != "actual" {
		formulaAmount = e.Amount
	}
	breakdown, err := calculators.BreakdownWith(b.card, e.CategoryCode, models.Audience(e.Audience), e.Payload)
	switch {
	case err != nil:
		row[8] = "Расчёт по методике недоступен: " + err.Error()
		row[calcColMethod] = formulaAmount
	case len(breakdown.Inputs) > calcInputPairs:
		row[8] = breakdown.Describe()
		row[calcColMethod] = formulaAmount
	case breakdown.Amount != formulaAmount:
		row[8] = fmt.Sprintf("%s. Сумма сохранена по другой редакции ставок (по действующим: %s руб.)", breakdown.Describe(), breakdown.Amount)
		row[calcColMethod] = formulaAmount
	default:
		row[8] = breakdown.Describe()
		for i, input := range breakdown.Inputs {
			labelCol, valueCol := 9+2*i, 10+2*i
			row[labelCol] = input.Label
			if input.IsRate() {
				rate, _ := money.FromRat(input.Value)
				if rateRow, ok := b.rateRow[input.RateCode]; ok {
					row[valueCol] = xlsx.Formula{Expr: xlsx.SheetRef(ratesSheetName, 2, rateRow), Value: rate}
				} else {
					row[valueCol] = rate
				}
			} else {
				row[valueCol] = ratFloat(input.Value)
			}
		}
		row[calcColMethod] = xlsx.Formula{
			Expr:  breakdown.Excel(func(index int) string { return xlsx.Ref(10+2*index, rowNum) }),
			Value: breakdown.Amount,
		}
	}
	method := "Средние значения Минцифры"
	switch {
	case e.CostMethod == "actual":
		method = "Фактические затраты (введены вручную)"
		row[calcColAmount] = e.Amount
	case e.Amount == formulaAmount:
		row[calcColAmount] = xlsx.Formula{Expr: xlsx.Ref(calcColMethod, rowNum), Value: e.Amount}
	default:
		row[calcColAmount] = e.Amount
	}
	row[7] = method
	b.rows = append(b.rows, row)
	b.amounts = append(b.amounts, e.Amount)
	b.periods = append(b.periods, e.Period)
	b.forms = append(b.forms, e.FormRow)
	return rowNum
}

// amountRef - ссылка на «Сумму в отчёте» строки rowNum листа расчёта.
func (b *calcBook) amountRef(rowNum int) string {
	return xlsx.SheetRef(calcSheetName, calcColAmount, rowNum)
}

func (b *calcBook) methodRef(rowNum int) string {
	return xlsx.SheetRef(calcSheetName, calcColMethod, rowNum)
}

// sumForm возвращает формулу суммы мероприятий строки формы formRow (и
// периода period, если он задан) и её значение.
func (b *calcBook) sumForm(formRow int, period string) (string, money.Amount) {
	expr := fmt.Sprintf("SUMIFS(%s,%s,%d", xlsx.SheetColumn(calcSheetName, calcColAmount), xlsx.SheetColumn(calcSheetName, calcColFormRow), formRow)
	if period != "" {
		expr += fmt.Sprintf(`,%s,"%s"`, xlsx.SheetColumn(calcSheetName, calcColPeriod), calcPeriodLabels[period])
	}
	expr += ")"
	var total money.Amount
	for i, form := range b.forms {
		if form == formRow && (period == "" || b.periods[i] == period) {
			total, _ = money.Add(total, b.amounts[i])
		}
	}
	return expr, total
}

// sumPeriod - сумма всех мероприятий периода по листу расчёта.
func (b *calcBook) sumPeriod(period string) (string, money.Amount) {
	expr := fmt.Sprintf(`SUMIFS(%s,%s,"%s")`, xlsx.SheetColumn(calcSheetName, calcColAmount), xlsx.SheetColumn(calcSheetName, calcColPeriod), calcPeriodLabels[period])
	var total money.Amount
	for i, value := range b.periods {
		if value == period {
			total, _ = money.Add(total, b.amounts[i])
		}
	}
	return expr, total
}

// write добавляет листы расчёта и ставок в конец книги.
func (b *calcBook) write(wb *xlsx.Workbook) {
	rows := append([][]interface{}{}, b.rows...)
	if len(b.rows) > 0 {
		var methodTotal, amountTotal money.Amount
		for _, row := range b.rows {
			methodTotal, _ = money.Add(methodTotal, cellAmount(row[calcColMethod]))
			amountTotal, _ = money.Add(amountTotal, cellAmount(row[calcColAmount]))
		}
		total := make([]interface{}, len(calcHeaders))
		for i := range total {
			total[i] = ""
		}
		total[1] = "ИТОГО"
		total[calcColMethod] = xlsx.Formula{Expr: xlsx.SumFormula(calcColMethod, 2, len(b.rows)+1), Value: methodTotal}
		total[calcColAmount] = xlsx.Formula{Expr: xlsx.SumFormula(calcColAmount, 2, len(b.rows)+1), Value: amountTotal}
		rows = append(rows, total)
	}
	wb.AddSheet(calcSheetName, calcHeaders, rows)
	rates := [][]interface{}{}
	for _, rate := range b.card.Rates() {
		label, unit := rate.Code, ""
		if definition, ok := tariffs.DefinitionOf(rate.Code); ok {
			label, unit = definition.Label, definition.Unit
		}
		amount, _ := money.FromRat(rate.Amount)
		rates = append(rates, []interface{}{rate.Code, label, amount, unit, rate.Source})
	}
	wb.AddSheet(ratesSheetName, []string{"Код ставки", "Наименование", "Ставка, руб.", "Единица", "Основание"}, rates)
}

func cellAmount(value interface{}) money.Amount {
	switch v := value.(type) {
	case money.Amount:
		return v
	case xlsx.Formula:
		return v.Value
	default:
		return 0
	}
}
