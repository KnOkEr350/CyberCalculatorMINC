package calculators

import (
	"fmt"
	"math"
	"math/big"
	"strings"

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
		{Key: "instruction_authority", Label: "Орган, выдавший поручение", Type: "select", Required: true, Options: []string{"president", "prime_minister", "deputy_prime_minister", "security_council"}},
		{Key: "instruction_reference", Label: "Реквизиты исходного поручения", Type: "text", Required: true, MaxLength: 1000},
		{Key: "implementation_deadline", Label: "Срок исполнения", Type: "date", Required: true},
		{Key: "activity_description", Label: "Описание мероприятия", Type: "text", Required: true, MaxLength: 2000},
		{Key: "metric_description", Label: "Метрика и объёмный показатель по Решению", Type: "text", Required: true, MaxLength: 2000},
		{Key: "metric_unit", Label: "Единица измерения", Type: "text", Required: true, MaxLength: 100},
		{Key: "planned_volume", Label: "Плановый объём", Type: "number", Minimum: 0},
		{Key: "actual_volume", Label: "Фактический объём", Type: "number", Required: true, Minimum: 0},
		{Key: "calculation_basis", Label: "Основание и методика расчёта", Type: "text", Required: true, MaxLength: 2000},
		{Key: "amount_manual", Label: "Сумма затрат, руб.", Type: "number", Required: true},
		{Key: "expense_evidence_reference", Label: "Акты, платежи и первичные документы", Type: "text", MaxLength: 2000},
	}
}

// Validate проверяет динамические показатели мероприятия по Решению
// Минцифры (MIN-02) и подтверждённую стоимость (MIN-03). Метрика, единица и
// объёмы задаются самим Решением, поэтому система не знает их наперёд — но
// обязана убедиться, что значения имеют правильный тип и смысл.
func (manualCalc) Validate(payload map[string]interface{}) error {
	unit, err := str(payload, "metric_unit")
	if err != nil {
		return fmt.Errorf("укажите единицу измерения объёма из Решения Минцифры")
	}
	// Единица измерения — это название («модуль ПО», «мероприятие»), а не
	// число: числовая единица означает, что поля перепутали местами.
	if _, numeric := new(big.Rat).SetString(strings.ReplaceAll(unit, ",", ".")); numeric {
		return fmt.Errorf("единица измерения должна быть названием, а не числом: %q", unit)
	}
	actual, err := positiveNum(payload, "actual_volume")
	if err != nil {
		return fmt.Errorf("укажите фактический объём мероприятия в единицах «%s»", unit)
	}
	if planned, ok := payload["planned_volume"]; ok && planned != nil {
		if _, err := nonNegativeNum(payload, "planned_volume"); err != nil {
			return fmt.Errorf("плановый объём должен быть неотрицательным числом")
		}
	}
	// MIN-03: в зачёт идёт фактически подтверждённая стоимость, поэтому она
	// обязана быть положительной и опираться на описанную методику.
	if _, err := positiveNum(payload, "amount_manual"); err != nil {
		return fmt.Errorf("укажите фактически подтверждённую стоимость мероприятия")
	}
	if _, err := str(payload, "calculation_basis"); err != nil {
		return fmt.Errorf("укажите основание и методику расчёта подтверждённой стоимости")
	}
	if actual <= 0 {
		return fmt.Errorf("фактический объём должен быть больше нуля")
	}
	return nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
