package handlers

import (
	"cybercalc/internal/models"
	"testing"
)

func TestRussianRegistryIdentifiers(t *testing.T) {
	if !validINN("1650084264") {
		t.Fatal("valid legal-entity INN rejected")
	}
	if validINN("1650084265") || validINN("123") {
		t.Fatal("invalid INN accepted")
	}
	if !validOGRN("1021602020384") {
		t.Fatal("valid OGRN rejected")
	}
	if validOGRN("1021602020385") || validOGRN("123") {
		t.Fatal("invalid OGRN accepted")
	}
	if !officialRegistryURL("https://islod.obrnadzor.gov.ru/rlic/details/example/") {
		t.Fatal("federal registry URL rejected")
	}
	for _, value := range []string{"http://islod.obrnadzor.gov.ru/file.xlsx", "https://obrnadzor.gov.ru.evil.test/file.xlsx", "https://user@obrnadzor.gov.ru/file.xlsx"} {
		if officialRegistryURL(value) {
			t.Fatalf("untrusted registry URL accepted: %s", value)
		}
	}
}

func TestNormalizeActiveAgreement(t *testing.T) {
	req := agreementWriteRequest{
		PartnerIDs:      []string{"partner-id"},
		AgreementKind:   "education_organization",
		Number:          "  15/26 ",
		Status:          "active",
		SignedOn:        "2026-01-10",
		ValidFrom:       "2026-01-01",
		ValidUntil:      "2026-12-31",
		SignatureMethod: "qualified_electronic",
		SignedBy:        "Иванов Иван Иванович",
		SignatureDate:   "2026-01-10",
		ResponsiblePeople: []models.AgreementResponsiblePerson{
			{Party: "cyberprotect", FullName: "Петров Пётр Петрович", Email: "petrov@example.ru"},
			{Party: "counterparty", FullName: "Сидоров Сидор Сидорович"},
		},
	}
	agreement, err := normalizeAgreement(req)
	if err != nil {
		t.Fatal(err)
	}
	if agreement.Request.Number != "15/26" {
		t.Fatalf("number was not normalized: %q", agreement.Request.Number)
	}

	req.ResponsiblePeople = req.ResponsiblePeople[:1]
	if _, err = normalizeAgreement(req); err == nil {
		t.Fatal("active agreement without both responsible parties accepted")
	}
	req.ResponsiblePeople = append(req.ResponsiblePeople, models.AgreementResponsiblePerson{Party: "counterparty", FullName: "Сидоров Сидор"})
	req.ValidUntil = "2025-12-31"
	if _, err = normalizeAgreement(req); err == nil {
		t.Fatal("agreement with reversed validity period accepted")
	}
}
