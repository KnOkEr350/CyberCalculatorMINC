package calculators

import (
	"math"

	"cybercalc/internal/models"
)

// manualCalc — категории «Реализация по Решению Минцифры», «ИТ-кружки для
// школьников», «Образовательный контент»: ни ТЗ, ни приказ не задают ставку
// или формулу для них (см. calculator.go, комментарий вверху файла).
// Пользователь указывает итоговую сумму затрат самостоятельно, обязательно
// сопроводив комментарием (это уже требование ТЗ для любого редактирования)
// и, для факта, подтверждающим документом.
type manualCalc struct{}

func (manualCalc) Calculate(_ models.Audience, payload map[string]interface{}) (float64, error) {
	amount, err := num(payload, "amount_manual")
	if err != nil {
		return 0, err
	}
	return round2(amount), nil
}

func (manualCalc) Fields() []FieldSpec {
	return []FieldSpec{
		{Key: "org_name", Label: "Наименование ОО", Type: "select"},
		{Key: "activity_description", Label: "Описание мероприятия", Type: "text", Required: true},
		{Key: "amount_manual", Label: "Сумма затрат, руб.", Type: "number", Required: true},
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
