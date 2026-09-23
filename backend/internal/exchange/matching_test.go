package exchange

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// CRYPTO-06: правила нормализации закреплены таблицей. Одинаковые по смыслу
// записи получают один ключ, разные — разные.
func TestKeyNormalizationRules(t *testing.T) {
	base := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	baseKey, err := KeyOf(base)
	if err != nil {
		t.Fatal(err)
	}
	if baseKey.Category != "teachers" || baseKey.Person != "иванов иван иванович" ||
		baseKey.Subject != "разработка по" || baseKey.Specialty != "09.03.01" || baseKey.Period != "3" {
		t.Fatalf("составной ключ: %+v", baseKey)
	}

	same := map[string]Record{
		"регистр":        teacherRecord("ИВАНОВ иван ИВАНОВИЧ", "разработка ПО", 64),
		"лишние пробелы": teacherRecord("  Иванов   Иван\tИванович ", "Разработка   ПО ", 64),
		"ё и е":          teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		"пунктуация в названии": teacherRecord("Иванов Иван Иванович", "Разработка, ПО.", 64),
		"кавычки и дефисы":      teacherRecord("Иванов-Иван «Иванович»", "«Разработка» ПО", 64),
	}
	// «ё»/«е» проверяется отдельной парой: в базовой записи ё нет.
	yo, _ := KeyOf(teacherRecord("Пётр Петров", "Курс", 1))
	ye, _ := KeyOf(teacherRecord("Петр Петров", "Курс", 1))
	if yo != ye {
		t.Errorf("«ё» и «е» должны совпадать: %+v и %+v", yo, ye)
	}
	for name, record := range same {
		key, err := KeyOf(record)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "кавычки и дефисы" {
			// Дефис между словами разделяет их: «Иванов-Иван» — два слова.
			if key.Person != "иванов иван иванович" {
				t.Errorf("%s: ФИО %q", name, key.Person)
			}
			continue
		}
		if key != baseKey {
			t.Errorf("%s: ключ %+v отличается от базового %+v", name, key, baseKey)
		}
	}

	different := map[string]Record{
		"другое имя":  teacherRecord("Иванов Иван Петрович", "Разработка ПО", 64),
		"инициалы":    teacherRecord("Иванов И. И.", "Разработка ПО", 64),
		"другой курс": teacherRecord("Иванов Иван Иванович", "Базы данных", 64),
	}
	otherSemester := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	otherSemester.Payload["semester"] = 4
	different["другой семестр"] = otherSemester
	otherSpecialty := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	otherSpecialty.Payload["specialty_code"] = "09.03.02"
	different["другая специальность"] = otherSpecialty
	for name, record := range different {
		key, err := KeyOf(record)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key == baseKey {
			t.Errorf("%s: разные записи получили один ключ", name)
		}
	}
}

// Число из JSON, из БД и из Go — одно и то же значение ключа: «3», 3, 3.0 и
// json.Number("3") дают один семестр.
func TestKeyTreatsNumbersUniformly(t *testing.T) {
	var keys []Key
	for _, semester := range []interface{}{3, int64(3), 3.0, json.Number("3"), json.Number("3.0"), "3", " 3 "} {
		record := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
		record.Payload["semester"] = semester
		key, err := KeyOf(record)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	for _, key := range keys[1:] {
		if key != keys[0] {
			t.Fatalf("представления семестра дали разные ключи: %+v и %+v", keys[0], key)
		}
	}
}

// Ключ строится из полей своего вида; прежние названия полей подхватываются.
func TestKeyFieldsPerCategory(t *testing.T) {
	cases := map[string]struct {
		record Record
		want   Key
	}{
		"стажировка": {internshipRecord("Сидорова Анна Сергеевна"),
			Key{Category: "internship", Specialty: "09.03.02", Person: "сидорова анна сергеевна", Subject: "3", Period: "2026-02-01..2026-04-30"}},
		"ООП/РПД: вид документа и работы входят в предмет": {
			Record{CategoryCode: "ood_rpd", Payload: map[string]interface{}{
				"program_name": "Прикладная информатика", "doc_type": "rpd", "activity_type": "expertise",
				"expert_full_name": "Смирнов К. В.", "specialty_code": "09.03.03"}},
			Key{Category: "ood_rpd", Specialty: "09.03.03", Person: "смирнов к в", Subject: "прикладная информатика rpd expertise"}},
		"ТОП-ИТ": {
			Record{CategoryCode: "top_it", Payload: map[string]interface{}{"project_name": "Проект «Старт»", "program_wave": 2}},
			Key{Category: "top_it", Subject: "проект старт", Period: "2"}},
		"решение МЦ, прежнее поле": {
			Record{CategoryCode: "minc_decision", Payload: map[string]interface{}{"decision_reference": "№ 15-р"}},
			Key{Category: "minc_decision", Subject: "15 р"}},
		"платформа": {
			Record{CategoryCode: "edu_content", Payload: map[string]interface{}{
				"platform_name": "Платформа", "digital_trace_period_start": "2026-01-01", "digital_trace_period_end": "2026-05-01"}},
			Key{Category: "edu_content", Subject: "платформа", Period: "2026-01-01..2026-05-01"}},
	}
	for name, tc := range cases {
		got, err := KeyOf(tc.record)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: ключ %+v, ожидался %+v", name, got, tc.want)
		}
	}
	if s := (Key{Category: "a", Person: "b"}).String(); !strings.Contains(s, "a") || !strings.Contains(s, "b") {
		t.Errorf("строковая запись ключа: %q", s)
	}
}

// Запись, которую нельзя сопоставить, — ошибка: пустой ключ совпал бы со всеми
// такими же записями сразу.
func TestRecordWithoutMatchKeyIsRejected(t *testing.T) {
	if _, err := KeyOf(Record{CategoryCode: "teachers", Payload: map[string]interface{}{"academic_hours": 64}}); !errors.Is(err, ErrNoMatchKey) {
		t.Fatalf("запись без ФИО и предмета: %v", err)
	}
	if _, err := KeyOf(Record{CategoryCode: "нет-такого"}); err == nil {
		t.Fatal("вид вне обмена должен отклоняться")
	}
	// Хватает одного из двух: предмета без ФИО достаточно для ТОП-ИТ.
	if _, err := KeyOf(Record{CategoryCode: "top_it", Payload: map[string]interface{}{"project_name": "Проект"}}); err != nil {
		t.Fatalf("предмета достаточно: %v", err)
	}
}

func TestIndexByKeyRefusesDuplicates(t *testing.T) {
	records := []Record{teacherRecord("Иванов Иван Иванович", "Курс", 1), teacherRecord("иванов иван иванович", "КУРС", 2)}
	_, duplicates, err := IndexByKey(records)
	if !errors.Is(err, ErrDuplicateKey) || len(duplicates) != 1 {
		t.Fatalf("дубликат ключа: err=%v дубликатов=%d", err, len(duplicates))
	}
	index, none, err := IndexByKey(sampleRecords())
	if err != nil || len(none) != 0 || len(index) != 3 {
		t.Fatalf("набор без дубликатов: %v %d", err, len(index))
	}
}
