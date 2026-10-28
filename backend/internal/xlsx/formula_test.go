package xlsx

import (
	"strings"
	"testing"

	"cybercalc/internal/money"
)

func TestFormulaCellKeepsExpressionAndCachedValue(t *testing.T) {
	sheet := Sheet{Headers: []string{"a"}, Rows: [][]interface{}{{Formula{Expr: SumFormula(0, 2, 3), Value: money.Amount(1050)}}}}
	xml := sheet.sheetXML()
	if !strings.Contains(xml, `<f>SUM(A2:A3)</f><v>10.50</v>`) {
		t.Fatalf("formula cell missing: %s", xml)
	}
}
