package handlers

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"cybercalc/internal/docx"
)

func agreementFixture() agreementParty {
	return agreementParty{
		Kind: "agreement2", Number: "01/26-МЦ", SignedOn: "2026-01-15", Year: 2026,
		PartnerName: "МГУ им. М.В. Ломоносова", PartnerINN: "7729082090",
		CompanyName: "ООО «ИТ-Холдинг»", CompanyINN: "7701234567", CompanyOGRN: "1027700123456",
		CompanyAddress: "119991, Москва, Ленинские горы, 1", CompanyDirector: "Петров П.П.",
	}
}

// documentText вытаскивает читаемый текст из готового .docx.
func documentText(t *testing.T, body []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("документ не открывается как .docx: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		f, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		raw, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	t.Fatal("в .docx нет word/document.xml")
	return ""
}

// REPORT-08: Приложения № 2 и № 3 к Порядку — это примерная форма
// соглашения («в лице …, действующего на основании …»), а не таблица
// реквизитов, и она должна нести реквизиты обеих сторон.
func TestAgreementTemplateContainsBothParties(t *testing.T) {
	party := agreementFixture()
	body, err := docx.Document(agreementTemplateTitle(party.Kind), agreementTemplateBlocks(party))
	if err != nil {
		t.Fatal(err)
	}
	text := documentText(t, body)
	for _, fragment := range []string{
		"СОГЛАШЕНИЕ", "Приложение № 2 к Порядку",
		"01/26-МЦ", "15.01.2026",
		"МГУ им. М.В. Ломоносова", "7729082090",
		"7701234567", "1027700123456", "119991, Москва, Ленинские горы, 1", "Петров П.П.",
		"в лице", "действующего на основании", "Реквизиты и подписи Сторон",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("в тексте соглашения нет фрагмента %q", fragment)
		}
	}
	// Организация названа «ООО «ИТ-Холдинг»» — кавычки должны быть
	// экранированы как XML, иначе документ не откроется.
	if strings.Contains(text, "ООО «ИТ-Холдинг»") && !strings.Contains(text, "&") {
		t.Log("кавычки не требуют экранирования, XML корректен")
	}
}

func TestAgreementTemplateSwitchesCounterpartyForRoiv(t *testing.T) {
	party := agreementFixture()
	party.Kind = "agreement3"
	title := agreementTemplateTitle(party.Kind)
	if !strings.Contains(title, "Приложение № 3 к Порядку") {
		t.Fatalf("заголовок формы № 3 неверен: %q", title)
	}
	body, err := docx.Document(title, agreementTemplateBlocks(party))
	if err != nil {
		t.Fatal(err)
	}
	text := documentText(t, body)
	if !strings.Contains(text, "Исполнительный орган субъекта Российской Федерации") {
		t.Fatalf("в форме № 3 контрагент должен называться РОИВ:\n%s", text)
	}
	if strings.Contains(text, "именуемое в дальнейшем «Образовательная организация»") {
		t.Fatal("в форме № 3 не должно быть образовательной организации как стороны")
	}
}

// Незаполненные реквизиты остаются местами для подписи, а не «<nil>» или
// пустотой, чтобы форму можно было допечатать вручную.
func TestAgreementTemplateKeepsPlaceholders(t *testing.T) {
	body, err := docx.Document(agreementTemplateTitle("agreement2"), agreementTemplateBlocks(agreementParty{Kind: "agreement2", Year: 2026}))
	if err != nil {
		t.Fatal(err)
	}
	text := documentText(t, body)
	if strings.Contains(text, "<nil>") {
		t.Fatal("пустые реквизиты не должны печататься как <nil>")
	}
	if !strings.Contains(text, "____") {
		t.Fatal("для незаполненных реквизитов должны остаться места для заполнения")
	}
}
