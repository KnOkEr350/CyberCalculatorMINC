package tests

import (
	"testing"

	"cybercalc/internal/xlsx"
)

// FuzzWorkbookReader checks the untrusted XLSX import boundary. Invalid ZIP/XML
// data must be rejected as an error, never panic or allocate without a bound.
func FuzzWorkbookReader(f *testing.F) {
	book := xlsx.New()
	book.AddSheet("Данные", []string{"course_name", "academic_hours"}, [][]interface{}{{"Безопасность", 2}})
	valid, err := book.Bytes()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte("not a zip archive"))
	f.Add([]byte("PK\x03\x04"))

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = xlsx.ReadFirst(data)
	})
}
