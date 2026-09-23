package compliance

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// DocumentMetadata — реквизиты типизированного документа (DATA-12): номер,
// дата и сведения о подписи и сертификате подписанта. Все поля необязательны,
// но сведения о подписи задаются только целиком.
type DocumentMetadata struct {
	Number                string
	Date                  string // YYYY-MM-DD
	SignerName            string
	CertificateSerial     string
	CertificateValidFrom  string // YYYY-MM-DD
	CertificateValidUntil string // YYYY-MM-DD
}

// Signed сообщает, описана ли подпись.
func (m DocumentMetadata) Signed() bool {
	return m.SignerName != "" || m.CertificateSerial != "" || m.CertificateValidFrom != "" || m.CertificateValidUntil != ""
}

var certificateSerialPattern = regexp.MustCompile(`^[0-9A-F]{6,64}$`)

// NormalizeDocumentMetadata приводит реквизиты к каноническому виду и проверяет
// их. today — московская дата проверки: документ из будущего невозможен.
//
// Главная связь — подпись и дата: документ подписан сертификатом, который на
// дату документа должен был действовать. Подпись просроченным или ещё не
// выданным сертификатом не подтверждает ничего, даже если сам сертификат
// подлинный.
func NormalizeDocumentMetadata(in DocumentMetadata, today time.Time) (DocumentMetadata, error) {
	out := DocumentMetadata{
		Number:     strings.TrimSpace(in.Number),
		Date:       strings.TrimSpace(in.Date),
		SignerName: strings.Join(strings.Fields(in.SignerName), " "),
		// Серийный номер пишут с пробелами и двоеточиями; хранится сплошной
		// строкой заглавных шестнадцатеричных цифр.
		CertificateSerial:     normalizeSerial(in.CertificateSerial),
		CertificateValidFrom:  strings.TrimSpace(in.CertificateValidFrom),
		CertificateValidUntil: strings.TrimSpace(in.CertificateValidUntil),
	}
	if len([]rune(out.Number)) > 200 {
		return out, fmt.Errorf("номер документа не длиннее 200 символов")
	}
	docDate, err := optionalDate(out.Date, "дата документа")
	if err != nil {
		return out, err
	}
	if !docDate.IsZero() {
		if docDate.Year() < 2000 {
			return out, fmt.Errorf("дата документа не раньше 2000 года")
		}
		if docDate.After(today) {
			return out, fmt.Errorf("дата документа не может быть в будущем")
		}
	}
	if !out.Signed() {
		return out, nil
	}
	switch {
	case out.SignerName == "":
		return out, fmt.Errorf("сведения о подписи задаются целиком: не указан подписант")
	case out.CertificateSerial == "":
		return out, fmt.Errorf("сведения о подписи задаются целиком: не указан серийный номер сертификата")
	case out.CertificateValidFrom == "" || out.CertificateValidUntil == "":
		return out, fmt.Errorf("сведения о подписи задаются целиком: не указан срок действия сертификата")
	}
	if l := len([]rune(out.SignerName)); l < 2 || l > 300 {
		return out, fmt.Errorf("имя подписанта от 2 до 300 символов")
	}
	if !certificateSerialPattern.MatchString(out.CertificateSerial) {
		return out, fmt.Errorf("серийный номер сертификата — от 6 до 64 шестнадцатеричных цифр")
	}
	from, err := optionalDate(out.CertificateValidFrom, "начало действия сертификата")
	if err != nil {
		return out, err
	}
	until, err := optionalDate(out.CertificateValidUntil, "окончание действия сертификата")
	if err != nil {
		return out, err
	}
	if until.Before(from) {
		return out, fmt.Errorf("сертификат не может закончиться раньше, чем начаться")
	}
	if !docDate.IsZero() && (docDate.Before(from) || docDate.After(until)) {
		return out, fmt.Errorf("на дату документа %s сертификат подписанта не действовал (срок %s — %s)",
			out.Date, out.CertificateValidFrom, out.CertificateValidUntil)
	}
	return out, nil
}

func optionalDate(value, label string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s указывается как ГГГГ-ММ-ДД", label)
	}
	return parsed, nil
}

func normalizeSerial(value string) string {
	var out strings.Builder
	for _, r := range value {
		if unicode.IsSpace(r) || r == ':' || r == '-' {
			continue
		}
		out.WriteRune(unicode.ToUpper(r))
	}
	return out.String()
}
