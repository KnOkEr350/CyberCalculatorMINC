package exchange

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// sheetText возвращает текст листа книги: значения хранятся строками в XML.
func sheetText(t *testing.T, book []byte, name string) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(book), int64(len(book)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	t.Fatalf("в книге нет листа %s", name)
	return ""
}

func protocolFixture(t *testing.T) (ProtocolSource, Result) {
	t.Helper()
	f := newFixture(t)
	imported, err := f.open(f.export(t, []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 40),
		teacherRecord("Смирнова Ольга Павловна", "Алгоритмы", 24),
	}))
	if err != nil {
		t.Fatal(err)
	}
	local := []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 32),
		teacherRecord("Кузнецов Дмитрий Иванович", "Сети", 16),
	}
	result, err := Diff(local, imported.Records)
	if err != nil {
		t.Fatal(err)
	}
	return ProtocolSource{
		Manifest: imported.Manifest, SenderOrgID: imported.Signer.OrganizationID, SenderSignerID: imported.Signer.SignerID,
		CheckedAt: fixtureNow, Operator: "Оператор Тестов", PackageSHA256: strings.Repeat("ab", 32),
	}, result
}

// CRYPTO-09: в протокол попадают все различия без исключения, а решения
// оператора — с причиной и автором.
func TestProtocolListsEveryDifferenceAndDecision(t *testing.T) {
	source, result := protocolFixture(t)
	collision := find(t, result, StatusCollision)[0]
	missingLocal := find(t, result, StatusMissingLocal)[0]
	decisions := map[string]Decision{
		collision.Key.String():    {Action: ActionAccept, Reason: "часы уточнены приказом № 12", By: "Оператор Тестов", At: fixtureNow},
		missingLocal.Key.String(): {Action: ActionAdd, Reason: "мероприятие проведено", By: "Оператор Тестов", At: fixtureNow},
	}
	book, err := BuildProtocol(source, result, decisions)
	if err != nil {
		t.Fatal(err)
	}

	summary := sheetText(t, book, "xl/worksheets/sheet1.xml")
	for _, want := range []string{"org-a", "signer-a", "Оператор Тестов", strings.Repeat("ab", 32), "Совпадают", "Различий без решения"} {
		if !strings.Contains(summary, want) {
			t.Errorf("в сводке нет %q", want)
		}
	}
	differences := sheetText(t, book, "xl/worksheets/sheet2.xml")
	// Все различия: расхождение по часам и сумме, запись «нет у нас», запись «нет в пакете».
	for _, want := range []string{
		"payload.academic_hours", "amount_rub", "Смирнова", "Кузнецов",
		"расхождение", "нет у нас", "нет в пакете",
		"принять входящее", "добавить", "часы уточнены приказом № 12", "мероприятие проведено",
	} {
		if !strings.Contains(differences, want) {
			t.Errorf("в перечне различий нет %q", want)
		}
	}
	// Различие без решения показано честно, а совпавшая запись в перечень не входит.
	if !strings.Contains(differences, "не принято") {
		t.Error("незакрытое различие должно быть помечено «не принято»")
	}
	if strings.Contains(differences, "Разработка ПО") {
		t.Error("совпавшая запись не должна попадать в перечень различий")
	}
}

// Протокол без единого решения тоже полон: он честно показывает, что решений нет.
func TestProtocolWithoutDecisionsMarksEverythingUndecided(t *testing.T) {
	source, result := protocolFixture(t)
	book, err := BuildProtocol(source, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	differences := sheetText(t, book, "xl/worksheets/sheet2.xml")
	// Четыре строки различий: часы, сумма, нет у нас, нет в пакете.
	if got := strings.Count(differences, "не принято"); got != 4 {
		t.Fatalf("строк без решения %d, ожидалось 4", got)
	}
	summary := sheetText(t, book, "xl/worksheets/sheet1.xml")
	if !strings.Contains(summary, "<v>3</v>") { // записей в пакете
		t.Error("в сводке нет числа записей пакета")
	}
}

// Значение, начинающееся с «=», в ячейке остаётся текстом: протокол открывают в
// Excel, и формула из чужого пакета выполняться не должна.
func TestProtocolNeverEmitsFormulas(t *testing.T) {
	source, _ := protocolFixture(t)
	local := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming.Payload["class_schedule"] = `=HYPERLINK("http://evil","клик")`
	result, err := Diff([]Record{local}, []Record{incoming})
	if err != nil {
		t.Fatal(err)
	}
	book, err := BuildProtocol(source, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	sheet := sheetText(t, book, "xl/worksheets/sheet2.xml")
	if strings.Contains(sheet, "<f>") || strings.Contains(sheet, "<f ") {
		t.Fatal("в протоколе не должно быть формул")
	}
	if !strings.Contains(sheet, "t=\"inlineStr\"") || !strings.Contains(sheet, "HYPERLINK") {
		t.Fatal("значение должно сохраниться как обычный текст")
	}
}

func TestProtocolShowsAmbiguousKeys(t *testing.T) {
	source, _ := protocolFixture(t)
	result, err := Diff(
		[]Record{teacherRecord("Иванов Иван Иванович", "Курс", 1), teacherRecord("иванов иван иванович", "курс", 2)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	book, err := BuildProtocol(source, result, map[string]Decision{
		result.Items[0].Key.String(): {Action: ActionReject, Reason: "дубль", By: "Оператор", At: time.Time{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sheet := sheetText(t, book, "xl/worksheets/sheet2.xml")
	for _, want := range []string{"неоднозначный ключ", "повторяется среди локальных записей", "оставить локальное", "дубль"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("в перечне нет %q", want)
		}
	}
}
