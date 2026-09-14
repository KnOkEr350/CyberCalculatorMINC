package handlers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/xlsx"
)

func testITRegistry(t *testing.T, count int, delimiter rune) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	w.Comma = delimiter
	w.Write(itCompanyColumns)
	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("77%07d", i)
		sum := 0
		for j, weight := range []int{2, 4, 10, 3, 5, 9, 4, 6, 8} {
			sum += int(prefix[j]-'0') * weight
		}
		inn := fmt.Sprintf("%s%d", prefix, sum%11%10)
		ogrnPrefix := int64(126770000000) + int64(i)
		ogrn := fmt.Sprintf("%d%d", ogrnPrefix, ogrnPrefix%11%10)
		w.Write([]string{fmt.Sprintf("ООО «Компания %d; ИТ»", i), inn, ogrn, "", "active", inn, time.Now().Format("2006-01-02"), "https://www.gosuslugi.ru/itorgs", "Строка 1\nСтрока 2"})
	}
	w.Flush()
	if w.Error() != nil {
		t.Fatal(w.Error())
	}
	return b.Bytes()
}

func TestITRegistryReadsBeyondThreeAndBeyondOnePage(t *testing.T) {
	for _, separator := range []rune{',', ';'} {
		rows, err := ReadITCompanies(testITRegistry(t, 650, separator))
		if err != nil || len(rows) != 650 {
			t.Fatalf("separator %q: got %d rows: %v", separator, len(rows), err)
		}
	}
}

func TestITRegistryRejectsInvalidLateRow(t *testing.T) {
	data := testITRegistry(t, 650, ',')
	// Corruption after the first 500 companies must reject the whole snapshot.
	data = append(data, []byte("broken,123,456,,,,,,\n")...)
	if rows, err := ReadITCompanies(data); err == nil || rows != nil {
		t.Fatal("accepted a partial corrupt snapshot")
	}
}

func TestITRegistryDeduplicatesIdenticalRowsAndRejectsConflicts(t *testing.T) {
	data := testITRegistry(t, 1, ',')
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	rows, _ := reader.ReadAll()
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.WriteAll(append(rows, rows[1]))
	w.Flush()
	if got, err := ReadITCompanies(b.Bytes()); err != nil || len(got) != 1 {
		t.Fatalf("identical duplicate: %v %v", got, err)
	}
	rows[1][0] = "Другая компания"
	w.Write(rows[1])
	w.Flush()
	if _, err := ReadITCompanies(b.Bytes()); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
}

func TestITRegistryAcceptsXLSXAndRejectsStaleActiveRecords(t *testing.T) {
	row := []interface{}{"Тестовая компания", "7707083893", "1027700132195", "", "active", "test", time.Now().Format("2006-01-02"), "https://www.gosuslugi.ru/itorgs", ""}
	wb := xlsx.New()
	wb.AddSheet("Реестр", itCompanyLabels, [][]interface{}{row})
	data, _ := wb.Bytes()
	if got, err := ReadITCompanies(data); err != nil || len(got) != 1 {
		t.Fatalf("xlsx: %v", err)
	}
	row[6] = time.Now().AddDate(0, 0, -36).Format("2006-01-02")
	wb = xlsx.New()
	wb.AddSheet("Реестр", itCompanyLabels, [][]interface{}{row})
	data, _ = wb.Bytes()
	if _, err := ReadITCompanies(data); err == nil {
		t.Fatal("stale accreditation accepted")
	}
}
