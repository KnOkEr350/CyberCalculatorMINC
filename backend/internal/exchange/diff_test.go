package exchange

import (
	"encoding/json"
	"testing"
)

func find(t *testing.T, result Result, status Status) []Item {
	t.Helper()
	items := []Item{}
	for _, item := range result.Items {
		if item.Status == status {
			items = append(items, item)
		}
	}
	return items
}

// CRYPTO-07: четыре исхода сравнения и значения обеих сторон в каждом.
func TestDiffClassifiesEveryRecord(t *testing.T) {
	local := []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64), // совпадёт
		teacherRecord("Петров Пётр Петрович", "Базы данных", 32),   // расхождение
		teacherRecord("Кузнецов Дмитрий Иванович", "Сети", 16),     // нет в пакете
	}
	incoming := []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 40),
		teacherRecord("Смирнова Ольга Павловна", "Алгоритмы", 24), // нет у нас
	}
	result, err := Diff(local, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusIdentical] != 1 || result.Counts[StatusCollision] != 1 ||
		result.Counts[StatusMissingLocal] != 1 || result.Counts[StatusMissingIncoming] != 1 {
		t.Fatalf("счётчики: %v", result.Counts)
	}
	total := 0
	for _, count := range result.Counts {
		total += count
	}
	if total != len(result.Items) || len(result.Items) != 4 {
		t.Fatalf("счётчики не сходятся с элементами: %d и %d", total, len(result.Items))
	}

	collision := find(t, result, StatusCollision)[0]
	if collision.Local == nil || collision.Incoming == nil {
		t.Fatal("при расхождении видны обе записи")
	}
	fields := map[string]FieldDiff{}
	for _, diff := range collision.Differences {
		fields[diff.Field] = diff
	}
	if len(fields) != 2 {
		t.Fatalf("расхождений %d, ожидалось 2 (часы и сумма): %+v", len(fields), collision.Differences)
	}
	if hours := fields["payload.academic_hours"]; hours.Local != 32 || hours.Incoming != 40 {
		t.Fatalf("часы: локально %v, во входящем %v", hours.Local, hours.Incoming)
	}
	if amount := fields["amount_rub"]; amount.Local != "132480.00" || amount.Incoming != "165600.00" {
		t.Fatalf("сумма: %+v", amount)
	}

	if missing := find(t, result, StatusMissingLocal)[0]; missing.Incoming == nil || missing.Local != nil {
		t.Fatal("для «нет у нас» видна только входящая запись")
	}
	if missing := find(t, result, StatusMissingIncoming)[0]; missing.Local == nil || missing.Incoming != nil {
		t.Fatal("для «нет в пакете» видна только локальная запись")
	}
}

// Идентификаторы экземпляра — не различие: у другого экземпляра они другие в
// каждой записи, и иначе всё было бы расхождением.
func TestDiffIgnoresInstanceIdentifiers(t *testing.T) {
	local := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming.Payload["org_name"] = "совсем-другой-uuid"
	incoming.Payload["staff_member_id"] = "другой-uuid-сотрудника"
	incoming.Payload["mentor_id"] = "ещё-один-uuid"

	result, err := Diff([]Record{local}, []Record{incoming})
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusIdentical] != 1 {
		t.Fatalf("идентификаторы экземпляра не должны давать расхождений: %+v", result.Items[0].Differences)
	}
}

// Сравнение идёт по смыслу: 64 и 64.0 — одно число, пустая строка, null и
// отсутствие поля — одно «не заполнено».
func TestDiffComparesValuesByMeaning(t *testing.T) {
	local := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming.Payload["academic_hours"] = json.Number("64.0")
	incoming.AmountRub = "264960"
	local.Payload["faculty"] = ""
	incoming.Payload["faculty"] = nil
	local.Payload["note"] = "  "
	incoming.Payload["employee_position"] = ""

	result, err := Diff([]Record{local}, []Record{incoming})
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusIdentical] != 1 {
		t.Fatalf("представление не должно давать расхождений: %+v", result.Items[0].Differences)
	}

	// Разница в строке с пробелами по краям — не расхождение, в содержимом — расхождение.
	incoming.Payload["department"] = "Кафедра ИС"
	local.Payload["department"] = " Кафедра ИС "
	if r, _ := Diff([]Record{local}, []Record{incoming}); r.Counts[StatusIdentical] != 1 {
		t.Fatal("пробелы по краям не различие")
	}
	incoming.Payload["department"] = "Кафедра ВТ"
	r, _ := Diff([]Record{local}, []Record{incoming})
	if r.Counts[StatusCollision] != 1 || r.Items[0].Differences[0].Field != "payload.department" {
		t.Fatalf("различие в содержимом должно находиться: %+v", r.Items)
	}
	// Число и строка с тем же текстом — разные значения.
	incoming.Payload["department"] = "Кафедра ИС"
	local.Payload["students_reach"] = 30
	incoming.Payload["students_reach"] = "тридцать"
	if r, _ := Diff([]Record{local}, []Record{incoming}); r.Counts[StatusCollision] != 1 {
		t.Fatal("число и слово — разные значения")
	}
}

// Поле, заполненное с одной стороны, — расхождение с локальным и входящим
// значением; отсутствие поля с одной стороны видно как пусто.
func TestDiffReportsFieldPresentOnOneSide(t *testing.T) {
	local := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming.Payload["class_schedule"] = "пн 10:00"

	result, err := Diff([]Record{local}, []Record{incoming})
	if err != nil {
		t.Fatal(err)
	}
	diffs := result.Items[0].Differences
	if len(diffs) != 1 || diffs[0].Field != "payload.class_schedule" || diffs[0].Local != nil || diffs[0].Incoming != "пн 10:00" {
		t.Fatalf("расхождение по полю: %+v", diffs)
	}
}

// Повтор ключа внутри одной стороны — отдельный статус, а не «последняя запись
// побеждает».
func TestDiffFlagsAmbiguousKeys(t *testing.T) {
	local := []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("иванов иван иванович", "РАЗРАБОТКА ПО", 32),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 32),
	}
	incoming := []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 32),
		teacherRecord("Петров Пётр Петрович", "БАЗЫ ДАННЫХ", 8),
	}
	result, err := Diff(local, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusAmbiguous] != 2 || len(result.Items) != 2 {
		t.Fatalf("неоднозначные ключи должны выделяться и не участвовать в остальном: %v", result.Counts)
	}
	details := map[string]bool{}
	for _, item := range find(t, result, StatusAmbiguous) {
		details[item.Detail] = true
	}
	if len(details) != 2 {
		t.Fatalf("поясняется, с какой стороны повтор: %v", details)
	}
}

func TestDiffIsDeterministicAndRejectsUnkeyedRecords(t *testing.T) {
	first, err := Diff(sampleRecords(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, _ := Diff(sampleRecords(), nil)
		for j := range first.Items {
			if first.Items[j].Key != again.Items[j].Key {
				t.Fatal("порядок результата должен быть стабильным")
			}
		}
	}
	if _, err := Diff([]Record{{CategoryCode: "teachers", Payload: map[string]interface{}{}}}, nil); err == nil {
		t.Fatal("запись без ключа не сравнивается")
	}
	empty, err := Diff(nil, nil)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("пустое сравнение: %v", err)
	}
}

// Записи сопоставляются по нормализованному ключу, но значения сравниваются как
// введены: запись, набранная с другим регистром, находится, а различие в
// написании остаётся видимым расхождением, которое решает человек.
func TestKeyMatchDoesNotHideDifferentSpelling(t *testing.T) {
	local := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	incoming := teacherRecord("иванов иван иванович", "разработка по", 64)
	result, err := Diff([]Record{local}, []Record{incoming})
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusCollision] != 1 || len(result.Items) != 1 {
		t.Fatalf("та же запись в другом написании должна находиться как расхождение: %v", result.Counts)
	}
	fields := map[string]bool{}
	for _, diff := range result.Items[0].Differences {
		fields[diff.Field] = true
	}
	if !fields["payload.teacher_full_name"] || !fields["payload.course_name"] {
		t.Fatalf("различия в написании должны быть видны: %v", fields)
	}
}
