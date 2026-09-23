package calculators

import (
	"strings"
	"testing"

	"cybercalc/internal/models"
)

func schoolProgramPayload() map[string]interface{} {
	return map[string]interface{}{
		"org_name": "school-id", "program_name": "Основы Python и ИИ",
		"academic_hours": 72.0, "developed_programs_count": 1.0, "students_count": 45.0,
		"class_range":    "5-11",
		"funding_source": "100% средства ИТ-компании", "budget_funding": "absent", "citizen_funding": "absent",
	}
}

// SCH-02: ИТ-кружки Вида 6 засчитываются только для 5–11 классов
// (`programs×530890 + hours×4260`).
func TestSchoolProgramClassRange(t *testing.T) {
	calc, err := Get("it_clubs")
	if err != nil {
		t.Fatal(err)
	}
	validator := calc.(payloadValidator)
	allowed := []string{"5-11", "7", "5, 6, 7", "8—9", "с 10 по 11", ""}
	for _, value := range allowed {
		payload := schoolProgramPayload()
		payload["class_range"] = value
		if err := validator.Validate(payload); err != nil {
			t.Fatalf("классы %q должны приниматься: %v", value, err)
		}
	}
	rejected := map[string]string{
		"1-4":  "5–11",
		"4-11": "5–11",
		"12":   "5–11",
		"0":    "5–11",
		"нет":  "числами",
	}
	for value, want := range rejected {
		payload := schoolProgramPayload()
		payload["class_range"] = value
		err := validator.Validate(payload)
		if err == nil {
			t.Fatalf("классы %q не должны приниматься для Вида 6", value)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("классы %q: ошибка %q не содержит %q", value, err.Error(), want)
		}
	}
	// Формула Вида 6: 1 программа + 72 часа = 837 610 ₽ (пример ТЗ).
	amount, err := calc.Calculate(models.AudienceSchool, schoolProgramPayload())
	if err != nil {
		t.Fatal(err)
	}
	if amount != 530890+72*4260 {
		t.Fatalf("сумма Вида 6 = %.2f, ожидалось 837610", amount)
	}
}

// SCH-03: Вид 7 — разработка программы плюс часы на каждого обученного
// учителя (`programs×1408570 + hours×teachers×3790`).
func TestTeacherTrainingFormulaCountsHoursPerTeacher(t *testing.T) {
	calc, err := Get("teacher_training")
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]interface{}{
		"developed_programs_count": 1.0, "academic_hours_per_teacher": 72.0, "trained_teachers_count": 10.0,
	}
	amount, err := calc.Calculate(models.AudienceSchool, base)
	if err != nil {
		t.Fatal(err)
	}
	if amount != 1408570+72*10*3790 {
		t.Fatalf("сумма Вида 7 = %.2f, ожидалось 1408570 + 3790×72×10", amount)
	}
	// Удвоение числа учителей удваивает часовую часть, разработка не меняется.
	base["trained_teachers_count"] = 20.0
	doubled, err := calc.Calculate(models.AudienceSchool, base)
	if err != nil {
		t.Fatal(err)
	}
	if doubled-1408570 != 2*(amount-1408570) {
		t.Fatalf("часовая часть должна зависеть от числа учителей: %.2f и %.2f", amount, doubled)
	}
	// Программа без обученных учителей не считается мероприятием (ADR-12).
	if _, err := calc.Calculate(models.AudienceSchool, map[string]interface{}{
		"developed_programs_count": 0.0, "academic_hours_per_teacher": 72.0, "trained_teachers_count": 0.0,
	}); err == nil {
		t.Fatal("без программы и без обученных учителей запись не должна считаться")
	}
}

// SCH-04: Вид 8 — человеко-месяцы доступа: школьник 6 800 ₽, учитель 8 590 ₽.
func TestEducationalContentCountsPersonMonths(t *testing.T) {
	calc, err := Get("edu_content")
	if err != nil {
		t.Fatal(err)
	}
	amount, err := calc.Calculate(models.AudienceSchool, map[string]interface{}{
		"student_platform_months": 100.0, "teacher_platform_months": 10.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if amount != 100*6800+10*8590 {
		t.Fatalf("сумма Вида 8 = %.2f, ожидалось 100×6800 + 10×8590", amount)
	}
	if _, err := calc.Calculate(models.AudienceSchool, map[string]interface{}{
		"student_platform_months": 0.0, "teacher_platform_months": 0.0,
	}); err == nil {
		t.Fatal("без месяцев доступа запись не должна считаться")
	}
	// Школьные виды не применяются к вузам и колледжам.
	for _, audience := range []models.Audience{models.AudienceVuz, models.AudienceKolledj} {
		if _, err := calc.Calculate(audience, map[string]interface{}{"student_platform_months": 1.0, "teacher_platform_months": 0.0}); err == nil {
			t.Fatalf("аудитория %q не должна допускаться для Вида 8", audience)
		}
	}
}

func ministryPayload() map[string]interface{} {
	return map[string]interface{}{
		"org_name": "partner-id", "decision_reference": "№ МЦ-П12-402 от 12.03.2026",
		"instruction_type": "government_instruction", "instruction_authority": "prime_minister", "instruction_reference": "Поручение ПР-1",
		"decision_number": "МЦ-П12-402", "decision_date": "2026-03-12",
		"implementation_start": "2026-03-15", "implementation_deadline": "2026-11-15",
		"implementation_conditions": "Передать результат по акту", "activity_description": "Разработка национальной СУБД",
		"metric_description": "Количество переданных подсистем", "metric_unit": "модуль ПО",
		"planned_volume": 2.0, "actual_volume": 2.0,
		"calculation_basis": "Акт сдачи-приёмки и платёжные поручения", "amount_manual": 300000.0,
		"decision_required_documents": "Акт сдачи-приёмки\nПлатёжное поручение",
		"decision_provided_documents": "Акт сдачи-приёмки\nПлатёжное поручение",
	}
}

// MIN-02: единица измерения, плановый и фактический объём проверяются по
// типу значения. MIN-03: в зачёт идёт только подтверждённая стоимость.
func TestMinistryDecisionDynamicMetrics(t *testing.T) {
	calc, err := Get("minc_decision")
	if err != nil {
		t.Fatal(err)
	}
	validator, ok := calc.(payloadValidator)
	if !ok {
		t.Fatal("для мероприятий по Решению Минцифры должен быть валидатор")
	}
	if err := validator.Validate(ministryPayload()); err != nil {
		t.Fatalf("корректное мероприятие должно проходить: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(map[string]interface{})
		wantErr string
	}{
		{"нет единицы измерения", func(p map[string]interface{}) { delete(p, "metric_unit") }, "единицу измерения"},
		{"единица задана числом", func(p map[string]interface{}) { p["metric_unit"] = "12" }, "названием, а не числом"},
		{"нет фактического объёма", func(p map[string]interface{}) { delete(p, "actual_volume") }, "фактический объём"},
		{"нулевой фактический объём", func(p map[string]interface{}) { p["actual_volume"] = 0.0 }, "фактический объём"},
		{"отрицательный плановый объём", func(p map[string]interface{}) { p["planned_volume"] = -1.0 }, "неотрицательным"},
		{"нет подтверждённой стоимости", func(p map[string]interface{}) { delete(p, "amount_manual") }, "подтверждённую стоимость"},
		{"нулевая стоимость", func(p map[string]interface{}) { p["amount_manual"] = 0.0 }, "подтверждённую стоимость"},
		{"нет методики расчёта", func(p map[string]interface{}) { delete(p, "calculation_basis") }, "методику расчёта"},
		{"вид поручения не соответствует органу", func(p map[string]interface{}) { p["instruction_type"] = "president_instruction" }, "не соответствует"},
		{"срок раньше начала", func(p map[string]interface{}) { p["implementation_deadline"] = "2026-03-01" }, "проверьте даты"},
		{"нет состава документов Решения", func(p map[string]interface{}) { delete(p, "decision_required_documents") }, "состав подтверждающих документов"},
		{"лишний документ", func(p map[string]interface{}) { p["decision_provided_documents"] = "Счёт-фактура" }, "отсутствует в составе"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := ministryPayload()
			tc.mutate(payload)
			err := validator.Validate(payload)
			if err == nil {
				t.Fatalf("ожидали ошибку %q, запись принята", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ошибка %q не содержит %q", err.Error(), tc.wantErr)
			}
		})
	}

	// Плановый объём необязателен: Решение может задать только факт.
	payload := ministryPayload()
	delete(payload, "planned_volume")
	if err := validator.Validate(payload); err != nil {
		t.Fatalf("мероприятие без планового объёма должно приниматься: %v", err)
	}
	// В зачёт идёт именно подтверждённая стоимость, а не объём.
	amount, err := calc.Calculate(models.AudienceVuz, ministryPayload())
	if err != nil {
		t.Fatal(err)
	}
	if amount != 300000 {
		t.Fatalf("в зачёт пошла сумма %.2f, ожидалась подтверждённая 300000", amount)
	}
}

// PRA-05: практика с трудоустройством переиспользует расчётный модуль
// стажировки, а не копирует формулу.
func TestPracticeSharesInternshipFormula(t *testing.T) {
	internship, err := Get("internship")
	if err != nil {
		t.Fatal(err)
	}
	practice, err := Get("employment_practice")
	if err != nil {
		t.Fatal(err)
	}
	for _, hours := range []struct{ student, mentor, months float64 }{
		{80, 20, 3}, {120, 30, 2}, {10, 3, 1}, {0.5, 0.25, 1},
	} {
		payload := map[string]interface{}{
			"student_load_hours_per_month": hours.student,
			"mentor_load_hours_per_month":  hours.mentor,
			"duration_months":              hours.months,
		}
		left, err := internship.Calculate(models.AudienceVuz, payload)
		if err != nil {
			t.Fatal(err)
		}
		right, err := practice.Calculate(models.AudienceVuz, payload)
		if err != nil {
			t.Fatal(err)
		}
		if left != right {
			t.Fatalf("формулы разошлись: стажировка %.2f, практика %.2f", left, right)
		}
	}
}
