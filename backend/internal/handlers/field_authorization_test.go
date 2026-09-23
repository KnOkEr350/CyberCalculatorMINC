package handlers

import (
	"encoding/json"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
)

func specialist(role models.Role) middleware.AuthUser {
	company := "11111111-1111-1111-1111-111111111111"
	return middleware.AuthUser{ID: "user", Role: role,
		EntityType: models.EntityOrganization, ITCompanyID: &company}
}

// SEC-05 (ТЗ §7.5): договоры стажёров, зарплаты и входящие справки правит
// только кадровая служба, исходящие справки прикладывает куратор, финансист
// работает только с компенсацией преподавателям, а юрист имеет доступ на
// чтение и не меняет ничего.
func TestSpecialistRolesOwnOnlyTheirFields(t *testing.T) {
	cases := []struct {
		role     models.Role
		category string
		document string
		upload   bool
		edit     bool
	}{
		// Кадры: договоры стажёров и входящие справки.
		{models.RoleHRSpecialist, "internship", "labor_contract", true, true},
		{models.RoleHRSpecialist, "internship", "incoming_certificate", true, true},
		{models.RoleHRSpecialist, "internship", "mentor_order", true, true},
		{models.RoleHRSpecialist, "employment_practice", "labor_contract", true, true},
		// Исходящая справка — зона куратора, а не кадров.
		{models.RoleHRSpecialist, "internship", "outgoing_certificate", false, true},
		{models.RoleCurator, "internship", "outgoing_certificate", true, true},
		// Кадры не касаются Вида 1.
		{models.RoleHRSpecialist, "teachers", "payment_order", false, false},
		// Финансист — только компенсация преподавателям.
		{models.RoleFinancialSpecialist, "teachers", "payment_order", true, true},
		{models.RoleFinancialSpecialist, "internship", "labor_contract", false, false},
		// Юрист: доступ на чтение, без загрузки и правки.
		{models.RoleLegalSpecialist, "internship", "labor_contract", false, false},
		{models.RoleLegalSpecialist, "teachers", "payment_order", false, false},
		// Аудитор тоже только смотрит.
		{models.RoleAuditorViewer, "internship", "labor_contract", false, false},
	}
	for _, tc := range cases {
		user := specialist(tc.role)
		if got := canUploadDocument(user, tc.category, tc.document); got != tc.upload {
			t.Errorf("%s: загрузка %s для вида %s = %v, ожидалось %v",
				tc.role, tc.document, tc.category, got, tc.upload)
		}
		if got := canEditEntryCategory(user, tc.category); got != tc.edit {
			t.Errorf("%s: правка вида %s = %v, ожидалось %v", tc.role, tc.category, got, tc.edit)
		}
	}

	// Юрист не проходит и общую проверку на загрузку: read-only по ТЗ.
	if canUploadAnyDocument(specialist(models.RoleLegalSpecialist)) {
		t.Error("юрист не загружает документы")
	}
	if canEditAnyEntry(specialist(models.RoleLegalSpecialist)) {
		t.Error("юрист не правит мероприятия")
	}
	if canCreateAnyEntry(specialist(models.RoleLegalSpecialist)) {
		t.Error("юрист не создаёт мероприятия")
	}
}

// Финансист двигает только поля выплаты. Смена аудитории, соглашения, метода
// расчёта, фактической суммы или содержательной части мероприятия — не его
// полномочия, даже внутри «своего» Вида 1.
func TestFinancialSpecialistTouchesOnlyPaymentFields(t *testing.T) {
	base := map[string]interface{}{
		"academic_hours": 64.0, "discipline": "Разработка ПО",
		"compensation_quarter": "Q1", "payment_status": "planned",
	}
	oldPayload, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	actual := money.Amount(100000)

	payload := func(changes map[string]interface{}) map[string]interface{} {
		result := map[string]interface{}{}
		for key, value := range base {
			result[key] = value
		}
		for key, value := range changes {
			result[key] = value
		}
		return result
	}

	t.Run("отметка об оплате требует дату и номер поручения", func(t *testing.T) {
		paidWithoutProof := updateEntryRequest{Payload: payload(map[string]interface{}{"payment_status": "paid"})}
		if financialUpdateAllowed(oldPayload, paidWithoutProof, "vuz", "a1", "average", &actual) {
			t.Fatal("отметка «оплачено» без даты и номера поручения не должна приниматься")
		}
		paid := updateEntryRequest{Payload: payload(map[string]interface{}{
			"payment_status": "paid", "payment_date": "2026-04-15", "payment_order_reference": "ПП-14",
		})}
		if !financialUpdateAllowed(oldPayload, paid, "vuz", "a1", "average", &actual) {
			t.Fatal("подтверждённая выплата с датой и номером должна приниматься")
		}
	})

	t.Run("содержательные и финансовые поля не меняются", func(t *testing.T) {
		forbidden := []struct {
			name    string
			request updateEntryRequest
		}{
			{"часы", updateEntryRequest{Payload: payload(map[string]interface{}{"academic_hours": 128.0})}},
			{"дисциплина", updateEntryRequest{Payload: payload(map[string]interface{}{"discipline": "Другая"})}},
			{"аудитория", updateEntryRequest{Audience: "kolledj", Payload: payload(nil)}},
			{"соглашение", updateEntryRequest{AgreementID: "a2", Payload: payload(nil)}},
			{"метод расчёта", updateEntryRequest{CostMethod: "actual", Payload: payload(nil)}},
		}
		for _, tc := range forbidden {
			if financialUpdateAllowed(oldPayload, tc.request, "vuz", "a1", "average", &actual) {
				t.Errorf("финансист не может менять %s", tc.name)
			}
		}
		other := money.Amount(999999)
		if financialUpdateAllowed(oldPayload, updateEntryRequest{ActualAmountRub: &other, Payload: payload(nil)}, "vuz", "a1", "average", &actual) {
			t.Error("финансист не может переписывать фактическую сумму")
		}
	})

	t.Run("поля выплаты меняются свободно", func(t *testing.T) {
		allowed := updateEntryRequest{Payload: payload(map[string]interface{}{
			"compensation_quarter": "Q2", "planned_compensation_rub": 50000.0,
		})}
		if !financialUpdateAllowed(oldPayload, allowed, "vuz", "a1", "average", &actual) {
			t.Fatal("квартал и плановая компенсация — поля финансиста")
		}
	})
}
