package compliance

import "testing"

func TestTeacherReadinessCannotBeOverriddenByDocuments(t *testing.T) {
	got := Evaluate("teachers", "fact", map[string]interface{}{"okz_code": "2512", "it_experience_days": 364, "class_schedule": "Пн"}, []string{"employment_contract", "appointment_order", "individual_plan"})
	if got.State != "red" || got.Eligible {
		t.Fatalf("expected objective IT-experience block, got %+v", got)
	}
}

func TestTeacherReadinessGreen(t *testing.T) {
	got := Evaluate("teachers", "fact", map[string]interface{}{"okz_code": "2512", "it_experience_days": 365, "class_schedule": "Пн"}, []string{"employment_contract", "appointment_order", "individual_plan"})
	if got.State != "green" || !got.Ready || !got.Eligible {
		t.Fatalf("expected green, got %+v", got)
	}
}

func TestTopProgressUsesSpentNotTransferred(t *testing.T) {
	got := Evaluate("top_it", "fact", map[string]interface{}{"planned_cofinancing_amount_rub": 1000, "transferred_amount_rub": 1000, "actual_spent_amount_rub": 600}, []string{"top_agreement", "payment_order", "spending_act", "ano_letter"})
	if got.State != "red" {
		t.Fatalf("expected red below 70%% spent, got %+v", got)
	}
}

func TestDocumentTypeRegistry(t *testing.T) {
	if !ValidDocumentType("digital_trace") || ValidDocumentType("unknown") {
		t.Fatal("invalid document type registry")
	}
}

func TestRejectedTypedDocumentOverridesLegacyReference(t *testing.T) {
	got := Evaluate("employment_practice", "fact", map[string]interface{}{
		"labor_contract_type":            "fixed_term",
		"labor_contract_number":          "ТД-1",
		"labor_contract_reference":       "скан ТД-1",
		"practice_agreement_reference":   "ДП-1",
		"mentor_id":                      "mentor-1",
		"mentor_order_reference":         "П-1",
		"individual_program_reference":   "ИП-1",
		"outgoing_certificate_reference": "С-1",
	}, []string{"labor_contract:rejected"})
	if got.State != "red" || got.Eligible {
		t.Fatalf("rejected labor contract must block eligibility, got %+v", got)
	}
}

func TestPendingTypedDocumentCannotBecomeGreen(t *testing.T) {
	got := Evaluate("teachers", "fact", map[string]interface{}{
		"okz_code": "2512", "it_experience_days": 365, "class_schedule": "Пн",
	}, []string{"employment_contract:pending", "appointment_order:approved", "individual_plan:approved"})
	if got.State != "yellow" || got.Eligible {
		t.Fatalf("pending legal review must remain yellow, got %+v", got)
	}
}
