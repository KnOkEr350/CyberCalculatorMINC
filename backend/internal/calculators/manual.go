package calculators

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

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
		{Key: "instruction_type", Label: "Вид поручения", Type: "select", Required: true, Options: []string{"president_instruction", "government_instruction", "curator_instruction", "security_council_decision"}},
		{Key: "instruction_authority", Label: "Орган, выдавший поручение", Type: "select", Required: true, Options: []string{"president", "prime_minister", "deputy_prime_minister", "security_council"}},
		{Key: "instruction_reference", Label: "Реквизиты исходного поручения", Type: "text", Required: true, MaxLength: 1000},
		{Key: "decision_number", Label: "Номер Решения Минцифры", Type: "text", Required: true, MaxLength: 200},
		{Key: "decision_date", Label: "Дата Решения Минцифры", Type: "date", Required: true},
		{Key: "decision_reference", Label: "Сводные реквизиты Решения (формируются автоматически)", Type: "text", MaxLength: 1000},
		{Key: "implementation_start", Label: "Дата начала реализации", Type: "date", Required: true},
		{Key: "implementation_deadline", Label: "Срок исполнения", Type: "date", Required: true},
		{Key: "implementation_conditions", Label: "Условия реализации по Решению", Type: "text", Required: true, MaxLength: 4000},
		{Key: "activity_description", Label: "Описание мероприятия", Type: "text", Required: true, MaxLength: 2000},
		{Key: "metric_description", Label: "Метрика и объёмный показатель по Решению", Type: "text", Required: true, MaxLength: 2000},
		{Key: "metric_unit", Label: "Единица измерения", Type: "text", Required: true, MaxLength: 100},
		{Key: "planned_volume", Label: "Плановый объём", Type: "number", Minimum: 0},
		{Key: "actual_volume", Label: "Фактический объём", Type: "number", Required: true, Minimum: 0},
		{Key: "calculation_basis", Label: "Основание и методика расчёта", Type: "text", Required: true, MaxLength: 2000},
		{Key: "amount_manual", Label: "Сумма затрат, руб.", Type: "number", Required: true},
		{Key: "decision_required_documents", Label: "Документы, требуемые Решением (через ;)", Type: "text", Required: true, MaxLength: 4000},
		{Key: "decision_provided_documents", Label: "Предоставленные документы (через ;)", Type: "text", MaxLength: 4000},
		{Key: "expense_evidence_reference", Label: "Акты, платежи и первичные документы", Type: "text", MaxLength: 2000},
	}
}

// Validate проверяет динамические показатели мероприятия по Решению
// Минцифры (MIN-02) и подтверждённую стоимость (MIN-03). Метрика, единица и
// объёмы задаются самим Решением, поэтому система не знает их наперёд — но
// обязана убедиться, что значения имеют правильный тип и смысл.
func (manualCalc) Validate(payload map[string]interface{}) error {
	instructionType, err := str(payload, "instruction_type")
	if err != nil {
		return fmt.Errorf("укажите вид исходного поручения")
	}
	authority, err := str(payload, "instruction_authority")
	if err != nil {
		return fmt.Errorf("укажите орган, выдавший исходное поручение")
	}
	expectedAuthority := map[string]string{
		"president_instruction": "president", "government_instruction": "prime_minister",
		"curator_instruction": "deputy_prime_minister", "security_council_decision": "security_council",
	}
	if expectedAuthority[instructionType] != authority {
		return fmt.Errorf("вид поручения не соответствует указанному органу")
	}
	decisionNumber, err := str(payload, "decision_number")
	if err != nil {
		return fmt.Errorf("укажите номер Решения Минцифры")
	}
	decisionDateRaw, err := str(payload, "decision_date")
	if err != nil {
		return fmt.Errorf("укажите дату Решения Минцифры")
	}
	startRaw, err := str(payload, "implementation_start")
	if err != nil {
		return fmt.Errorf("укажите дату начала реализации мероприятия")
	}
	deadlineRaw, err := str(payload, "implementation_deadline")
	if err != nil {
		return fmt.Errorf("укажите срок исполнения мероприятия")
	}
	decisionDate, decisionErr := time.Parse("2006-01-02", decisionDateRaw)
	start, startErr := time.Parse("2006-01-02", startRaw)
	deadline, deadlineErr := time.Parse("2006-01-02", deadlineRaw)
	if decisionErr != nil || startErr != nil || deadlineErr != nil || deadline.Before(start) || deadline.Before(decisionDate) {
		return fmt.Errorf("проверьте даты Решения и периода реализации")
	}
	if _, err := str(payload, "implementation_conditions"); err != nil {
		return fmt.Errorf("укажите условия реализации из Решения Минцифры")
	}
	payload["decision_reference"] = fmt.Sprintf("№ %s от %s", decisionNumber, decisionDateRaw)
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
	required, err := evidenceItems(payload, "decision_required_documents")
	if err != nil || len(required) == 0 {
		return fmt.Errorf("перечислите состав подтверждающих документов, установленный Решением Минцифры")
	}
	provided, err := evidenceItems(payload, "decision_provided_documents")
	if err != nil {
		return err
	}
	requiredSet := make(map[string]bool, len(required))
	for _, item := range required {
		requiredSet[strings.ToLower(item)] = true
	}
	for _, item := range provided {
		if !requiredSet[strings.ToLower(item)] {
			return fmt.Errorf("предоставленный документ %q отсутствует в составе, заданном Решением", item)
		}
	}
	if actual <= 0 {
		return fmt.Errorf("фактический объём должен быть больше нуля")
	}
	return nil
}

func evidenceItems(payload map[string]interface{}, key string) ([]string, error) {
	raw, ok := payload[key]
	if !ok || raw == nil || strings.TrimSpace(fmt.Sprint(raw)) == "" {
		return nil, nil
	}
	text, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("поле %q должно быть списком строк", key)
	}
	text = strings.ReplaceAll(text, ";", "\n")
	items, seen := []string{}, map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		item := strings.Join(strings.Fields(line), " ")
		if item == "" {
			continue
		}
		normalized := strings.ToLower(item)
		if seen[normalized] {
			return nil, fmt.Errorf("список документов содержит дубль %q", item)
		}
		seen[normalized] = true
		items = append(items, item)
	}
	if len(items) > 20 {
		return nil, fmt.Errorf("в Решении допускается не более 20 типов подтверждающих документов")
	}
	return items, nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
