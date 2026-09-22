// Package docx produces an editable working table, not a signed statutory report.
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

func escapeText(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

// pack собирает минимальный .docx вокруг готового тела документа.
func pack(body string) ([]byte, error) {
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   body,
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

// Block — абзац текстового документа. Heading выделяет заголовок раздела,
// Text — обычный абзац. Пустой Text даёт пустую строку-разделитель.
type Block struct {
	Heading bool
	Text    string
}

// Heading и Paragraph — короткие конструкторы блоков.
func Heading(text string) Block   { return Block{Heading: true, Text: text} }
func Paragraph(text string) Block { return Block{Text: text} }

// Document собирает текстовый .docx (книжная ориентация) — форма соглашения
// и подобные документы, состоящие из абзацев, а не из таблицы.
func Document(title string, blocks []Block) ([]byte, error) {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	fmt.Fprintf(&doc, `<w:p><w:pPr><w:jc w:val="center"/></w:pPr><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escapeText(title))
	for _, block := range blocks {
		if block.Heading {
			fmt.Fprintf(&doc, `<w:p><w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escapeText(block.Text))
			continue
		}
		fmt.Fprintf(&doc, `<w:p><w:pPr><w:jc w:val="both"/></w:pPr><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, escapeText(block.Text))
	}
	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701"/></w:sectPr></w:body></w:document>`)
	return pack(doc.String())
}

func Table(title string, headers []string, rows [][]string) ([]byte, error) {
	escape := escapeText
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
	return pack(doc.String())
}
