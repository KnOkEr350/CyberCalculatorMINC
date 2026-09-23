package compliance

import (
	"strings"
	"testing"
	"time"
)

var metadataToday = time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

func signed() DocumentMetadata {
	return DocumentMetadata{
		Number: " № 15-Д ", Date: "2026-06-10", SignerName: "  Иванов   Иван Иванович ",
		CertificateSerial: "01:ab cd-ef 12", CertificateValidFrom: "2026-01-01", CertificateValidUntil: "2026-12-31",
	}
}

// DATA-12: реквизиты приводятся к каноническому виду.
func TestDocumentMetadataIsNormalized(t *testing.T) {
	got, err := NormalizeDocumentMetadata(signed(), metadataToday)
	if err != nil {
		t.Fatal(err)
	}
	if got.Number != "№ 15-Д" || got.SignerName != "Иванов Иван Иванович" || got.CertificateSerial != "01ABCDEF12" {
		t.Fatalf("нормализация: %+v", got)
	}
	if !got.Signed() {
		t.Fatal("подпись описана")
	}
}

// Документ без подписи и без реквизитов допустим: всё необязательно.
func TestUnsignedDocumentNeedsNoMetadata(t *testing.T) {
	for _, m := range []DocumentMetadata{{}, {Number: "б/н"}, {Date: "2026-01-01"}} {
		got, err := NormalizeDocumentMetadata(m, metadataToday)
		if err != nil || got.Signed() {
			t.Errorf("%+v: %v", m, err)
		}
	}
}

// Сведения о подписи задаются только целиком.
func TestSignatureMetadataMustBeComplete(t *testing.T) {
	base := signed()
	for name, mutate := range map[string]func(*DocumentMetadata){
		"нет подписанта":        func(m *DocumentMetadata) { m.SignerName = "" },
		"нет серийного номера":  func(m *DocumentMetadata) { m.CertificateSerial = "" },
		"нет начала срока":      func(m *DocumentMetadata) { m.CertificateValidFrom = "" },
		"нет окончания срока":   func(m *DocumentMetadata) { m.CertificateValidUntil = "" },
		"только подписант":      func(m *DocumentMetadata) { *m = DocumentMetadata{SignerName: "Иванов Иван"} },
		"только серийный номер": func(m *DocumentMetadata) { *m = DocumentMetadata{CertificateSerial: "ABCDEF"} },
	} {
		m := base
		mutate(&m)
		if _, err := NormalizeDocumentMetadata(m, metadataToday); err == nil {
			t.Errorf("%s: неполные сведения о подписи должны отклоняться", name)
		}
	}
}

// Подпись связана с датой: сертификат обязан действовать на дату документа.
func TestCertificateMustCoverTheDocumentDate(t *testing.T) {
	cases := map[string]struct {
		date string
		ok   bool
	}{
		"первый день срока":    {"2026-01-01", true},
		"последний день срока": {"2026-12-31", false}, // будущее относительно «сегодня»
		"внутри срока":         {"2026-06-10", true},
		"накануне начала":      {"2025-12-31", false},
	}
	for name, tc := range cases {
		m := signed()
		m.Date = tc.date
		_, err := NormalizeDocumentMetadata(m, metadataToday)
		if (err == nil) != tc.ok {
			t.Errorf("%s (%s): ошибка=%v, ожидалась успешная проверка=%v", name, tc.date, err, tc.ok)
		}
	}
	// Дата после окончания сертификата (в прошлом): сертификат уже не действовал.
	late := signed()
	late.CertificateValidUntil, late.Date = "2026-03-01", "2026-06-10"
	if _, err := NormalizeDocumentMetadata(late, metadataToday); err == nil || !strings.Contains(err.Error(), "не действовал") {
		t.Fatalf("документ позже окончания сертификата: %v", err)
	}
	// Без даты документа проверка срока не выполняется: сравнивать не с чем.
	noDate := signed()
	noDate.Date = ""
	if _, err := NormalizeDocumentMetadata(noDate, metadataToday); err != nil {
		t.Fatalf("без даты документа: %v", err)
	}
}

func TestMetadataFormatsAndBounds(t *testing.T) {
	for name, mutate := range map[string]func(*DocumentMetadata){
		"дата в будущем":                func(m *DocumentMetadata) { m.Date = "2026-09-24"; m.CertificateValidUntil = "2027-12-31" },
		"дата раньше 2000 года":         func(m *DocumentMetadata) { m.Date = "1999-12-31"; m.CertificateValidFrom = "1990-01-01" },
		"дата в другом формате":         func(m *DocumentMetadata) { m.Date = "10.06.2026" },
		"начало срока в другом формате": func(m *DocumentMetadata) { m.CertificateValidFrom = "01.01.2026" },
		"срок закончился до начала": func(m *DocumentMetadata) {
			m.CertificateValidFrom = "2026-12-31"
			m.CertificateValidUntil = "2026-01-01"
		},
		"серийный номер не hex":        func(m *DocumentMetadata) { m.CertificateSerial = "ZZZZZZZZ" },
		"серийный номер короткий":      func(m *DocumentMetadata) { m.CertificateSerial = "ABC" },
		"серийный номер длинный":       func(m *DocumentMetadata) { m.CertificateSerial = strings.Repeat("A", 65) },
		"номер длиннее 200":            func(m *DocumentMetadata) { m.Number = strings.Repeat("н", 201) },
		"имя подписанта в один символ": func(m *DocumentMetadata) { m.SignerName = "И" },
		"имя подписанта длиннее 300":   func(m *DocumentMetadata) { m.SignerName = strings.Repeat("и", 301) },
	} {
		m := signed()
		mutate(&m)
		if _, err := NormalizeDocumentMetadata(m, metadataToday); err == nil {
			t.Errorf("%s: должно отклоняться", name)
		}
	}
}
