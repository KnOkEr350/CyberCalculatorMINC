// Package xlsx — минимальный писатель .xlsx на стандартной библиотеке
// (archive/zip + encoding/xml). Поддерживает несколько листов с заголовком
// и строками из строк/чисел — этого достаточно для выгрузок ТЗ ("выгрузка
// в xls": план активностей, отчёт по мероприятиям, отчёт по стажировкам,
// каждый — по партнёру и сводный для МЦ). Сторонние библиотеки (excelize
// и т.п.) сознательно не используются.
package xlsx

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

type Sheet struct {
	Name    string
	Headers []string
	Rows    [][]interface{} // string | float64 | int
}

type Workbook struct {
	Sheets []Sheet
}

func New() *Workbook {
	return &Workbook{}
}

func (wb *Workbook) AddSheet(name string, headers []string, rows [][]interface{}) {
	wb.Sheets = append(wb.Sheets, Sheet{Name: sanitizeSheetName(name), Headers: headers, Rows: rows})
}

func sanitizeSheetName(name string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", "?", "", "*", "", "[", "(", "]", ")", ":", "-")
	n := replacer.Replace(name)
	if len(n) > 31 {
		n = n[:31]
	}
	if n == "" {
		n = "Sheet"
	}
	return n
}

func escapeXML(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// colLetter конвертирует 0-based номер колонки в букву(ы) Excel (0->A, 25->Z, 26->AA...).
func colLetter(n int) string {
	var s string
	for n >= 0 {
		s = string(rune('A'+n%26)) + s
		n = n/26 - 1
	}
	return s
}

func (s Sheet) sheetXML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	b.WriteString(`<sheetData>`)

	writeRow := func(rowIdx int, values []interface{}) {
		fmt.Fprintf(&b, `<row r="%d">`, rowIdx+1)
		for colIdx, v := range values {
			ref := fmt.Sprintf("%s%d", colLetter(colIdx), rowIdx+1)
			switch val := v.(type) {
			case float64:
				fmt.Fprintf(&b, `<c r="%s"><v>%v</v></c>`, ref, val)
			case int:
				fmt.Fprintf(&b, `<c r="%s"><v>%d</v></c>`, ref, val)
			default:
				fmt.Fprintf(&b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, escapeXML(fmt.Sprintf("%v", val)))
			}
		}
		b.WriteString(`</row>`)
	}

	headerRow := make([]interface{}, len(s.Headers))
	for i, h := range s.Headers {
		headerRow[i] = h
	}
	writeRow(0, headerRow)
	for i, row := range s.Rows {
		writeRow(i+1, row)
	}

	b.WriteString(`</sheetData></worksheet>`)
	return b.String()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
%s
<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>
</Relationships>`

const workbookRelsHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`

// WriteTo сериализует книгу в валидный .xlsx (OOXML) поток.
func (wb *Workbook) Encode(w io.Writer) error {
	zw := zip.NewWriter(w)

	write := func(name, content string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write([]byte(content))
		return err
	}

	var overrides strings.Builder
	var workbookSheetsXML strings.Builder
	var workbookRels strings.Builder
	workbookRels.WriteString(workbookRelsHeader)

	for i, s := range wb.Sheets {
		idx := i + 1
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, idx)
		fmt.Fprintf(&workbookSheetsXML, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escapeXML(s.Name), idx, idx)
		fmt.Fprintf(&workbookRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, idx, idx)
	}
	workbookRels.WriteString(`</Relationships>`)

	workbookXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets>%s</sheets>
</workbook>`, workbookSheetsXML.String())

	if err := write("[Content_Types].xml", fmt.Sprintf(contentTypesXML, overrides.String())); err != nil {
		return err
	}
	if err := write("_rels/.rels", rootRelsXML); err != nil {
		return err
	}
	if err := write("xl/workbook.xml", workbookXML); err != nil {
		return err
	}
	if err := write("xl/_rels/workbook.xml.rels", workbookRels.String()); err != nil {
		return err
	}
	for i, s := range wb.Sheets {
		if err := write(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), s.sheetXML()); err != nil {
			return err
		}
	}
	return zw.Close()
}

// Bytes — удобный помощник для HTTP-хендлеров.
func (wb *Workbook) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := wb.Encode(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
