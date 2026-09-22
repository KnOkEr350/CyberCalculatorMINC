package calculators

import (
	"strings"
	"testing"

	"cybercalc/internal/models"
)

// TOP-02: перечисленные и фактически списанные вузом средства ведутся
// раздельно, а в зачёт норматива 3% идёт именно списанная сумма
// (ТЗ §7.5: «принимается строго сумма, фактически списанная вузом»).
func TestTopITCountsSpentNotTransferred(t *testing.T) {
	calc, err := Get("top_it")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]interface{}{
		"cofinancing_amount_rub":  3000000.0,
		"transferred_amount_rub":  3000000.0,
		"actual_spent_amount_rub": 1800000.0,
	}
	amount, err := calc.Calculate(models.AudienceVuz, payload)
	if err != nil {
		t.Fatal(err)
	}
	if amount != 1800000 {
		t.Fatalf("в зачёт пошла сумма %.2f, ожидалась фактически списанная 1800000", amount)
	}
	// Та же сумма должна сохраняться в БД через точный путь.
	stored, err := CalculateAmount("top_it", models.AudienceVuz, payload)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Rubles() != 1800000 {
		t.Fatalf("в БД сохранится %s ₽ вместо списанной суммы", stored)
	}
}

// Без отдельного поля списания зачётной суммой служит объём софинансирования
// из принятого отчёта получателя гранта.
func TestTopITFallsBackToCofinancingReport(t *testing.T) {
	calc, _ := Get("top_it")
	amount, err := calc.Calculate(models.AudienceVuz, map[string]interface{}{"cofinancing_amount_rub": 950000.0})
	if err != nil {
		t.Fatal(err)
	}
	if amount != 950000 {
		t.Fatalf("сумма по отчёту о софинансировании = %.2f, ожидалось 950000", amount)
	}
}

// Списать больше перечисленного нельзя: это противоречие в первичных данных.
func TestTopITRejectsSpentAboveTransferred(t *testing.T) {
	calc, _ := Get("top_it")
	validator, ok := calc.(payloadValidator)
	if !ok {
		t.Fatal("для ТОП-ИТ должен быть валидатор")
	}
	err := validator.Validate(map[string]interface{}{
		"transferred_amount_rub": 1000000.0, "actual_spent_amount_rub": 1500000.0,
	})
	if err == nil {
		t.Fatal("списание больше перечисленного должно отклоняться")
	}
	if !strings.Contains(err.Error(), "превышать перечисленную") {
		t.Fatalf("непонятная причина отказа: %v", err)
	}
	// Равные суммы допустимы: вуз освоил всё перечисленное.
	if err := validator.Validate(map[string]interface{}{
		"transferred_amount_rub": 1000000.0, "actual_spent_amount_rub": 1000000.0,
	}); err != nil {
		t.Fatalf("полное освоение перечисленного должно приниматься: %v", err)
	}
}

// Вид 4 не применяется к колледжам и школам (ТЗ §7.1).
func TestTopITRejectsNonUniversityAudience(t *testing.T) {
	calc, _ := Get("top_it")
	for _, audience := range []models.Audience{models.AudienceKolledj, models.AudienceSchool} {
		if _, err := calc.Calculate(audience, map[string]interface{}{"cofinancing_amount_rub": 1000.0}); err == nil {
			t.Fatalf("аудитория %q не должна допускаться для ТОП-ИТ", audience)
		}
	}
}

// TCH-05: формы оформления привлечённого преподавателя перечислены в ТЗ §7.2.
func TestTeacherEmploymentFormsMatchOrder(t *testing.T) {
	calc, err := Get("teachers")
	if err != nil {
		t.Fatal(err)
	}
	var options []string
	for _, field := range calc.Fields() {
		if field.Key == "employment_form" {
			options = field.Options
		}
	}
	want := []string{
		"ТД по совместительству",
		"ГПХ",
		"Трудовой договор с ИТ-компанией",
		"Договор пожертвования",
		"Прямой договор между ОО и ИТ-компанией",
	}
	if len(options) != len(want) {
		t.Fatalf("форм оформления %d, ТЗ §7.2 перечисляет %d: %v", len(options), len(want), options)
	}
	for i, value := range want {
		if options[i] != value {
			t.Fatalf("форма %d = %q, ожидалась %q", i, options[i], value)
		}
	}
}
