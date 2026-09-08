package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestRoundTripAndSheetNames(t *testing.T) {
	wb := New()
	name := strings.Repeat("Университет ", 10)
	wb.AddSheet(name, []string{"ФИО", "Часы"}, [][]interface{}{{"Иванов & Иван", 2.5}})
	wb.AddSheet(name, []string{"test"}, nil)
	if wb.Sheets[0].Name == wb.Sheets[1].Name || len([]rune(wb.Sheets[0].Name)) > 31 {
		t.Fatal("bad sheet names")
	}
	data, err := wb.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadFirst(data)
	if err != nil {
		t.Fatal(err)
	}
	if rows[1][0] != "Иванов & Иван" || rows[1][1] != "2.5" {
		t.Fatalf("bad roundtrip: %v", rows)
	}
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range z.File {
		r, _ := f.Open()
		d := xml.NewDecoder(r)
		for {
			_, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: %v", f.Name, err)
			}
		}
		r.Close()
	}
}
func changePart(t *testing.T, data []byte, name, value string) []byte {
	t.Helper()
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	for _, f := range z.File {
		r, _ := f.Open()
		b, _ := io.ReadAll(r)
		r.Close()
		if f.Name == name {
			b = []byte(value)
		}
		dst, _ := w.Create(f.Name)
		dst.Write(b)
	}
	w.Close()
	return out.Bytes()
}
func TestRejectFormulaAndSparseBomb(t *testing.T) {
	wb := New()
	wb.AddSheet("Данные", []string{"hours"}, nil)
	data, _ := wb.Bytes()
	for _, sheet := range []string{
		`<worksheet><sheetData><row r="1"><c r="A1"><f>1+1</f><v>2</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1000000"><c r="A1000000"><v>2</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1"><c r="ZZZ1"><v>2</v></c></row></sheetData></worksheet>`,
		`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>999</v></c></row></sheetData></worksheet>`,
	} {
		if _, err := ReadFirst(changePart(t, data, "xl/worksheets/sheet1.xml", sheet)); err == nil {
			t.Fatal("unsafe workbook accepted")
		}
	}
}

func TestRichInlineAndNonFirstPhysicalSheet(t *testing.T) {
	wb := New()
	wb.AddSheet("one", []string{"header"}, nil)
	wb.AddSheet("two", []string{"second"}, nil)
	data, _ := wb.Bytes()
	data = changePart(t, data, "xl/workbook.xml", `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="two" sheetId="2" r:id="rId2"/></sheets></workbook>`)
	data = changePart(t, data, "xl/worksheets/sheet2.xml", `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><r><t>Фа</t></r><r><t>милия</t></r></is></c><c r="C1"><v>3</v></c></row></sheetData></worksheet>`)
	rows, e := ReadFirst(data)
	if e != nil {
		t.Fatal(e)
	}
	if rows[0][0] != "Фамилия" || rows[0][1] != "" || rows[0][2] != "3" {
		t.Fatalf("rich/sparse parsing: %v", rows)
	}
}
