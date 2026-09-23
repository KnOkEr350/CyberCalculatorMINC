package handlers

import (
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

func companyUser(role models.Role) middleware.AuthUser {
	return middleware.AuthUser{Role: role, EntityType: models.EntityOrganization}
}

func educationUser(role models.Role) middleware.AuthUser {
	return middleware.AuthUser{Role: role, EntityType: models.EntityEduInst}
}

// INT-02: скан трудового договора и первичную справку стажёра вносит только
// кадровая служба. Куратор ведёт методическую часть и к этим документам не
// допускается.
func TestIncomingHrDocumentsBelongToHr(t *testing.T) {
	hrOnly := []string{"labor_contract", "incoming_certificate", "mentor_order"}
	for _, category := range []string{"internship", "employment_practice"} {
		for _, documentType := range hrOnly {
			if !canUploadDocument(companyUser(models.RoleHRSpecialist), category, documentType) {
				t.Fatalf("%s/%s: кадровая служба должна вносить документ", category, documentType)
			}
			for _, role := range []models.Role{models.RoleCurator, models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer} {
				if canUploadDocument(companyUser(role), category, documentType) {
					t.Fatalf("%s/%s: роль %q не должна вносить кадровый документ", category, documentType, role)
				}
			}
		}
	}
}

// INT-05: итоговую справку об обучении прикрепляет куратор — и со стороны
// ИТ-компании, и со стороны образовательной организации.
func TestOutgoingCertificateBelongsToCurator(t *testing.T) {
	if !canUploadDocument(companyUser(models.RoleCurator), "internship", "outgoing_certificate") {
		t.Fatal("куратор ИТ-компании должен прикреплять итоговую справку")
	}
	if !canUploadDocument(educationUser(models.RoleCurator), "internship", "outgoing_certificate") {
		t.Fatal("куратор образовательной организации должен прикреплять итоговую справку")
	}
	for _, role := range []models.Role{models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer} {
		if canUploadDocument(companyUser(role), "internship", "outgoing_certificate") {
			t.Fatalf("роль %q не должна прикреплять итоговую справку", role)
		}
	}
	// Образовательная организация не вносит ничего, кроме итоговой справки.
	for _, documentType := range []string{"labor_contract", "incoming_certificate", "practice_agreement", "mentor_order"} {
		if canUploadDocument(educationUser(models.RoleCurator), "internship", documentType) {
			t.Fatalf("образовательная организация не должна вносить %q", documentType)
		}
	}
}

// PRA-02: договор о практической подготовке ведёт сторона ИТ-компании;
// контрагент его не подменяет.
func TestPracticeAgreementDocumentStaysWithCompany(t *testing.T) {
	if !canUploadDocument(companyUser(models.RoleCurator), "employment_practice", "practice_agreement") {
		t.Fatal("куратор ИТ-компании должен вносить договор о практической подготовке")
	}
	if canUploadDocument(educationUser(models.RoleCurator), "employment_practice", "practice_agreement") {
		t.Fatal("образовательная организация не вносит договор о практической подготовке")
	}
	if canUploadDocument(companyUser(models.RoleAuditorViewer), "employment_practice", "practice_agreement") {
		t.Fatal("аудитор не вносит документы")
	}
}

// SEC-06: AUDITOR_VIEWER — полностью read-only. Ни одна операция изменения
// данных ему недоступна.
func TestAuditorViewerIsReadOnly(t *testing.T) {
	auditor := companyUser(models.RoleAuditorViewer)
	if isStaff(auditor) {
		t.Fatal("аудитор не относится к сотрудникам, ведущим данные")
	}
	if canPrepareReports(auditor) {
		t.Fatal("аудитор не готовит отчёты")
	}
	if canCreateAnyEntry(auditor) {
		t.Fatal("аудитор не создаёт мероприятия")
	}
	if canEditAnyEntry(auditor) {
		t.Fatal("аудитор не редактирует мероприятия")
	}
	if canUploadAnyDocument(auditor) {
		t.Fatal("аудитор не загружает документы")
	}
	for _, category := range []string{"teachers", "ood_rpd", "internship", "employment_practice", "top_it", "minc_decision", "it_clubs", "teacher_training", "edu_content"} {
		if canCreateEntryCategory(auditor, category) {
			t.Fatalf("аудитор не должен создавать мероприятия вида %q", category)
		}
		if canEditEntryCategory(auditor, category) {
			t.Fatalf("аудитор не должен редактировать мероприятия вида %q", category)
		}
		for _, documentType := range []string{"labor_contract", "outgoing_certificate", "payment_order", "spending_act", "other"} {
			if canUploadDocument(auditor, category, documentType) {
				t.Fatalf("аудитор не должен вносить %q в %q", documentType, category)
			}
		}
	}
	if canReviewReport(auditor, string(models.PeriodPlan)) || canReviewReport(auditor, string(models.PeriodFact)) {
		t.Fatal("аудитор не согласует и не возвращает отчёты контрагента")
	}
	if canApproveReports(auditor) {
		t.Fatal("аудитор не утверждает отчёты")
	}
	if canManageITCompanies(auditor) || canManagePartnerStructure(auditor) || canProposeEducationDirectory(auditor) || canApproveEducationDirectory(auditor) {
		t.Fatal("аудитор не выполняет административные изменения справочников")
	}
	// При этом читать данные своей организации аудитор вправе.
	if !canReadTenantData(auditor) {
		t.Fatal("аудитор должен иметь инспекционный доступ на чтение")
	}
	if !canViewITCompanies(auditor) {
		t.Fatal("аудитор должен видеть справочник ИТ-компаний в режиме чтения")
	}
	if !canAccessPartner(auditor, "partner-id") {
		t.Fatal("аудитор должен иметь чтение карточек партнёров в своём tenant scope")
	}
}
