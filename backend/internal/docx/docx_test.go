package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"testing"
)

func TestDocumentXML(t *testing.T) {
	b, e := Table("Тест & <текст>", []string{"ФИО", "Сумма"}, [][]string{{"Иванов Иван", "100"}})
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		t.Fatal(e)
	}
	if len(z.File) != 3 {
		t.Fatal("missing OOXML parts")
	}
	for _, f := range z.File {
		r, _ := f.Open()
		d := xml.NewDecoder(r)
		for {
			_, e := d.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatalf("%s: %v", f.Name, e)
			}
		}
		r.Close()
	}
}
