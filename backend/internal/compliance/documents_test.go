package compliance

import "testing"

// OOP-03: комплект Вида 3 — проект программы, подписанное заключение
// компании и решение об утверждении образовательной организацией.
func TestProgramApprovalDocuments(t *testing.T) {
	required := map[string]string{
		"program_project":           "проект ООП/РПД",
		"expert_conclusion":         "подписанное заключение",
		"academic_council_protocol": "решение об утверждении",
	}
	full := Evaluate("ood_rpd", "fact", programPayload(), programDocuments())
	if full.State != "green" {
		t.Fatalf("полный комплект должен быть зелёным: %+v", full)
	}
	for document, description := range required {
		got := Evaluate("ood_rpd", "fact", programPayload(), without(programDocuments(), document))
		if got.State == "green" {
			t.Fatalf("без документа «%s» комплект не полон: %+v", description, got)
		}
		if len(got.Blocking)+len(got.Warnings) == 0 {
			t.Fatalf("отсутствие документа «%s» должно быть названо в причинах", description)
		}
	}
	// Свободная ссылка в реквизитах заменяет прикреплённый документ только
	// тогда, когда он не отклонён юридической проверкой.
	payload := programPayload()
	payload["project_document_reference"] = "Проект РПД от 12.02.2026"
	viaReference := Evaluate("ood_rpd", "fact", payload, without(programDocuments(), "program_project"))
	if viaReference.State == "red" {
		t.Fatalf("реквизиты проекта должны заменять скан: %+v", viaReference)
	}
	rejected := Evaluate("ood_rpd", "fact", payload, append(without(programDocuments(), "program_project"), "program_project:rejected"))
	if rejected.State != "red" {
		t.Fatalf("отклонённый юристом документ не может подтверждаться ссылкой: %+v", rejected)
	}
}

// TOP-09: комплект Вида 4 — договор софинансирования, платёжное поручение,
// акт о расходовании и письмо-согласование АНО АЦ.
func TestTopITAnoDocuments(t *testing.T) {
	full := Evaluate("top_it", "fact", topPayload(1000), topDocuments())
	if full.State != "green" {
		t.Fatalf("полный комплект ТОП-ИТ должен быть зелёным: %+v", full)
	}
	blocking := []string{"top_agreement", "payment_order", "spending_act"}
	for _, document := range blocking {
		got := Evaluate("top_it", "fact", topPayload(1000), without(topDocuments(), document))
		if got.State != "red" {
			t.Fatalf("без документа %q зачёт невозможен: %+v", document, got)
		}
	}
	// Письмо АНО АЦ не блокирует зачёт, но держит строку в зоне внимания.
	got := Evaluate("top_it", "fact", topPayload(1000), without(topDocuments(), "ano_letter"))
	if got.State != "yellow" {
		t.Fatalf("без письма АНО АЦ ожидалась жёлтая зона: %+v", got)
	}
	if got.Eligible {
		t.Fatalf("без согласования АНО АЦ строка не считается допустимой: %+v", got)
	}
}

// MIN-04: комплект Вида 5 задаётся самим Решением Минцифры — обязательны
// решение с исходным поручением и первичные документы о расходах.
func TestMinistryDecisionDocuments(t *testing.T) {
	documents := []string{"ministry_decision", "expense_evidence"}
	payload := ministryDecisionPayload()
	if got := Evaluate("minc_decision", "fact", payload, documents); got.State != "green" {
		t.Fatalf("полный комплект по Решению должен быть зелёным: %+v", got)
	}
	noDecision := Evaluate("minc_decision", "fact", payload, without(documents, "ministry_decision"))
	if noDecision.State != "red" {
		t.Fatalf("без Решения Минцифры мероприятие не засчитывается: %+v", noDecision)
	}
	noEvidence := Evaluate("minc_decision", "fact", payload, without(documents, "expense_evidence"))
	if noEvidence.State != "yellow" {
		t.Fatalf("без первичных документов ожидалась жёлтая зона: %+v", noEvidence)
	}
	// Плановая строка по Решению может опережать первичные документы.
	if plan := Evaluate("minc_decision", "plan", payload, nil); plan.State == "red" {
		t.Fatalf("плановая строка не должна блокироваться отсутствием документов: %+v", plan)
	}
	incomplete := ministryDecisionPayload()
	incomplete["decision_provided_documents"] = "Акт"
	if got := Evaluate("minc_decision", "fact", incomplete, documents); got.State != "yellow" {
		t.Fatalf("неполный динамический комплект должен оставаться жёлтым: %+v", got)
	}
	legacy := ministryDecisionPayload()
	delete(legacy, "decision_number")
	if got := Evaluate("minc_decision", "fact", legacy, documents); got.State != "red" {
		t.Fatalf("legacy-запись с неполной карточкой должна требовать ручной проверки: %+v", got)
	}
}

func ministryDecisionPayload() map[string]interface{} {
	return map[string]interface{}{
		"instruction_type": "government_instruction", "instruction_authority": "prime_minister",
		"instruction_reference": "Поручение ПР-1", "decision_number": "МЦ-1", "decision_date": "2026-03-12",
		"implementation_start": "2026-03-15", "implementation_deadline": "2026-11-15",
		"implementation_conditions": "Передать результат по акту", "activity_description": "Разработка СУБД",
		"decision_required_documents": "Акт\nПлатёжное поручение", "decision_provided_documents": "Акт\nПлатёжное поручение",
	}
}
