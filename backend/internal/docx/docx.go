// Package docx produces an editable working table, not a signed statutory report.
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

func Table(title string, headers []string, rows [][]string) ([]byte, error) {
	escape := func(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	fmt.Fprintf(&doc, `<w:p><w:r><w:rPr><w:b/></w:rPr><w:t>%s</w:t></w:r></w:p>`, escape(title))
	doc.WriteString(`<w:p><w:r><w:t>Рабочая выгрузка. Не заменяет согласованный отчёт по форме приказа.</w:t></w:r></w:p><w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders><w:top w:val="single" w:sz="4"/><w:left w:val="single" w:sz="4"/><w:bottom w:val="single" w:sz="4"/><w:right w:val="single" w:sz="4"/><w:insideH w:val="single" w:sz="4"/><w:insideV w:val="single" w:sz="4"/></w:tblBorders></w:tblPr>`)
	doc.WriteString(`<w:tblGrid>`)
	for range headers {
		doc.WriteString(`<w:gridCol w:w="2800"/>`)
	}
	doc.WriteString(`</w:tblGrid>`)
	write := func(values []string, header bool) {
		doc.WriteString(`<w:tr>`)
		if header {
			doc.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
		}
		for _, v := range values {
			doc.WriteString(`<w:tc><w:tcPr/><w:p><w:r>`)
			if header {
				doc.WriteString(`<w:rPr><w:b/></w:rPr>`)
			}
			fmt.Fprintf(&doc, `<w:t xml:space="preserve">%s</w:t></w:r></w:p></w:tc>`, escape(v))
		}
		doc.WriteString(`</w:tr>`)
	}
	write(headers, true)
	for _, row := range rows {
		write(row, false)
	}
	doc.WriteString(`</w:tbl><w:sectPr><w:pgSz w:w="16838" w:h="11906" w:orient="landscape"/><w:pgMar w:top="720" w:right="720" w:bottom="720" w:left="720"/></w:sectPr></w:body></w:document>`)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   doc.String(),
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for name, content := range files {
		f, e := z.Create(name)
		if e != nil {
			return nil, e
		}
		if _, e = f.Write([]byte(content)); e != nil {
			return nil, e
		}
	}
	if e := z.Close(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
