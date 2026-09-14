package handlers

import (
	"cybercalc/internal/models"
	"testing"
	"time"
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
	if !officialRegistryURL("https://egrul.nalog.ru/index.html") {
		t.Fatal("official EGRUL URL rejected")
	}
	for _, value := range []string{"http://islod.obrnadzor.gov.ru/file.xlsx", "https://obrnadzor.gov.ru.evil.test/file.xlsx", "https://user@obrnadzor.gov.ru/file.xlsx"} {
		if officialRegistryURL(value) {
			t.Fatalf("untrusted registry URL accepted: %s", value)
		}
	}
}

func TestDirectoryRejectsStaleRegistryData(t *testing.T) {
	header := []string{"name", "partner_kind", "region", "inn", "ogrn", "license_number", "license_status", "institution_status", "registry_record_id", "source_url", "registry_updated_at"}
	row := []string{"Тестовый вуз", "vuz", "г. Москва", "7707083893", "1027700132195", "Л035-ТЕСТ", "active", "active", "test-record", "https://islod.obrnadzor.gov.ru/rlic/details/test", time.Now().AddDate(0, 0, -36).Format("2006-01-02")}
	if _, errors := validateDirectoryRows([][]string{header, row}); len(errors) == 0 {
		t.Fatal("stale registry data accepted")
	}
	row[10] = time.Now().Format("2006-01-02")
	if valid, errors := validateDirectoryRows([][]string{header, row}); len(errors) != 0 || len(valid) != 1 {
		t.Fatalf("fresh registry data rejected: %v", errors)
	}
}

func TestDirectoryAcceptsRussianOfficeTemplate(t *testing.T) {
	header := make([]string, 0, len(directoryColumns))
	for _, column := range directoryColumns {
		header = append(header, column.Label)
	}
	row := []string{"Тестовый вуз", "Вуз", "г. Москва", "7707083893", "1027700132195", "Л035-ТЕСТ", "Действует", "Действует", "test-record-russian", "https://islod.obrnadzor.gov.ru/rlic/details/test", time.Now().Format("2006-01-02")}
	valid, errors := validateDirectoryRows([][]string{header, row})
	if len(errors) != 0 || len(valid) != 1 {
		t.Fatalf("Russian template rejected: %v", errors)
	}
	if valid[0][1] != "vuz" || valid[0][6] != "active" || valid[0][7] != "active" {
		t.Fatalf("Russian values were not normalized: %v", valid[0])
	}
}

func TestDirectoryWorkbookOnlyConfirmsMarkedRows(t *testing.T) {
	header := make([]string, 0, len(directoryColumns)+1)
	for _, column := range directoryColumns {
		header = append(header, column.Label)
	}
	header = append(header, "Действие")
	base := []string{"Тестовый вуз", "Вуз", "г. Москва", "7707083893", "1027700132195", "", "Не указан", "Действует", "test-record-action", "https://egrul.nalog.ru/index.html", time.Now().Format("2006-01-02")}
	skipped := append(append([]string{}, base...), "")
	confirmed := append(append([]string{}, base...), "Подтвердить")
	confirmed[0] = "Подтверждаемый вуз"
	confirmed[8] = "test-record-confirm"
	valid, errors := validateDirectoryRows([][]string{header, skipped, confirmed})
	if len(errors) != 0 || len(valid) != 1 {
		t.Fatalf("action column was not respected: valid=%d errors=%v", len(valid), errors)
	}
	if valid[0][0] != "Подтверждаемый вуз" || valid[0][6] != "unknown" {
		t.Fatalf("wrong confirmed row: %#v", valid[0])
	}
}

func TestNormalizeDirectoryReview(t *testing.T) {
	req := directoryWriteRequest{
		Name: "  Тестовый   вуз ", PartnerKind: "vuz", Region: " г. Москва ",
		INN: "7707083893", OGRN: "1027700132195", LicenseStatus: "unknown",
		InstitutionStatus: "active", SourceURL: "https://egrul.nalog.ru/index.html",
		RegistryUpdatedAt: time.Now().Format("2006-01-02"), Confirm: true,
	}
	if err := normalizeDirectoryWrite(&req); err != nil {
		t.Fatalf("valid confirmation rejected: %v", err)
	}
	if req.Name != "Тестовый вуз" || req.Region != "г. Москва" {
		t.Fatalf("values not normalized: %#v", req)
	}
	req.INN = ""
	if err := normalizeDirectoryWrite(&req); err == nil {
		t.Fatal("confirmation without INN accepted")
	}
}

func TestDirectoryRejectsCellsPastRussianHeaders(t *testing.T) {
	header := make([]string, 0, len(directoryColumns))
	for _, column := range directoryColumns {
		header = append(header, column.Label)
	}
	row := make([]string, len(directoryColumns)+1)
	row[0], row[len(row)-1] = "Тестовый вуз", "лишнее значение"
	if _, errors := validateDirectoryRows([][]string{header, row}); len(errors) == 0 {
		t.Fatal("data past headers accepted")
	}
}

func TestITCompanyValidation(t *testing.T) {
	req := itCompanyWriteRequest{
		Name: "ООО Ромашка", INN: "7707083893", OGRN: "1027700132195",
		AccreditationNumber: "АА-1", RegistryRecordID: "registry-1",
		RegistryUpdatedAt: time.Now().Format("2006-01-02"), SourceURL: "https://digital.gov.ru/ru/activity/govservices/1/",
	}
	if _, err := normalizeITCompany(req); err != nil {
		t.Fatalf("valid IT company rejected: %v", err)
	}
	req.SourceURL = "https://digital.gov.ru.evil.test/company"
	if _, err := normalizeITCompany(req); err == nil {
		t.Fatal("unofficial IT registry URL accepted")
	}
}

func TestOfficeValuesAreRussianAndReversible(t *testing.T) {
	for canonical, russian := range map[string]string{
		"rpd": "РПД", "vo": "Высшее образование", "development": "Разработка",
		"education_organization": "С образовательной организацией", "active": "Действует",
	} {
		if actual := officeValue(canonical); actual != russian {
			t.Fatalf("%s: got %q, want %q", canonical, actual, russian)
		}
		if actual := canonicalOfficeValue(russian, []string{canonical}); actual != canonical {
			t.Fatalf("%q was not mapped back to %q", russian, canonical)
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

func TestRegionalAuthorityValidation(t *testing.T) {
	req := regionalAuthorityWriteRequest{
		Name:      " Министерство образования тестового региона ",
		Region:    " Тестовый регион ",
		INN:       "7707083893",
		OGRN:      "1027700132195",
		Status:    "active",
		SourceURL: "https://education.example.gov.ru/authority",
	}
	normalized, err := normalizeRegionalAuthority(req)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Name != "Министерство образования тестового региона" {
		t.Fatal("regional authority name was not normalized")
	}
	req.SourceURL = "https://example.test/not-official"
	if _, err = normalizeRegionalAuthority(req); err == nil {
		t.Fatal("unofficial regional authority source accepted")
	}

	agreement := agreementWriteRequest{
		PartnerIDs: []string{"school-id"}, AgreementKind: "roiv", Number: "РОИВ-1", Status: "draft",
		SignedOn: "2026-01-01", ValidFrom: "2026-01-01", ValidUntil: "2026-12-31", SignatureMethod: "unsigned",
	}
	if _, err = normalizeAgreement(agreement); err == nil {
		t.Fatal("ROIV agreement without regional authority accepted")
	}
	agreement.RegionalAuthorityID = "authority-id"
	if _, err = normalizeAgreement(agreement); err != nil {
		t.Fatalf("ROIV agreement with authority rejected: %v", err)
	}
}
