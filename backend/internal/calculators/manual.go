package calculators

import (
	"math"

	"cybercalc/internal/models"
)

// manualCalc используется только для мероприятий по Решению Минцифры:
// метрика, ставка и методика определяются самим Решением.
type manualCalc struct{}

func (manualCalc) Calculate(_ models.Audience, payload map[string]interface{}) (float64, error) {
	amount, err := positiveNum(payload, "amount_manual")
	if err != nil {
		return 0, err
	}
	return round2(amount), nil
}

func (manualCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select", Required: true},
		{Key: "decision_reference", Label: "Реквизиты Решения Минцифры", Type: "text", Required: true},
		{Key: "activity_description", Label: "Описание мероприятия", Type: "text", Required: true, MaxLength: 2000},
		{Key: "metric_description", Label: "Метрика и объёмный показатель по Решению", Type: "text", Required: true, MaxLength: 2000},
		{Key: "calculation_basis", Label: "Основание и методика расчёта", Type: "text", Required: true, MaxLength: 2000},
		{Key: "amount_manual", Label: "Сумма затрат, руб.", Type: "number", Required: true},
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
