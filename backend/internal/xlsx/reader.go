package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// ReadFirst reads values, never executes formulas or fetches external links.
// Bounds apply to compressed input (HTTP) and uncompressed archive members.
func ReadFirst(data []byte) ([][]string, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("нужен файл .xlsx (не .xls и не CSV)")
	}
	if len(z.File) > 200 {
		return nil, fmt.Errorf("слишком много частей книги")
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		total += f.UncompressedSize64
		if f.UncompressedSize64 > 16<<20 || total > 32<<20 {
			return nil, fmt.Errorf("распакованная книга превышает 32 МБ")
		}
		if _, ok := files[f.Name]; ok {
			return nil, fmt.Errorf("дублирующая часть книги")
		}
		files[f.Name] = f
	}
	read := func(name string, v interface{}) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("часть книги %s отсутствует", name)
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		defer r.Close()
		return xml.NewDecoder(io.LimitReader(r, 16<<20)).Decode(v)
	}
	var book struct {
		Sheets []struct {
			ID string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
			Type   string `xml:"Type,attr"`
		} `xml:"Relationship"`
	}
	if err := read("xl/workbook.xml", &book); err != nil {
		return nil, err
	}
	if len(book.Sheets) == 0 {
		return nil, fmt.Errorf("книга без листов")
	}
	if err := read("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	sheetPath := ""
	for _, rel := range rels.Items {
		if rel.ID == book.Sheets[0].ID && rel.Mode != "External" && strings.HasSuffix(rel.Type, "/worksheet") {
			if strings.HasPrefix(rel.Target, "/") {
				sheetPath = strings.TrimPrefix(path.Clean(rel.Target), "/")
			} else {
				sheetPath = path.Join("xl", rel.Target)
			}
		}
	}
	if !strings.HasPrefix(sheetPath, "xl/") {
		return nil, fmt.Errorf("не удалось найти первый лист")
	}
	type richText struct {
		Text string `xml:"t"`
		Runs []struct {
			Text string `xml:"t"`
		} `xml:"r"`
	}
	value := func(t richText) string {
		s := t.Text
		for _, r := range t.Runs {
			s += r.Text
		}
		return s
	}
	shared := []string{}
	if files["xl/sharedStrings.xml"] != nil {
		var s struct {
			Items []richText `xml:"si"`
		}
		if err := read("xl/sharedStrings.xml", &s); err != nil {
			return nil, err
		}
		for _, item := range s.Items {
			shared = append(shared, value(item))
		}
	}
	var sheet struct {
		Rows []struct {
			Number int `xml:"r,attr"`
			Cells  []struct {
				Ref     string   `xml:"r,attr"`
				Type    string   `xml:"t,attr"`
				Value   string   `xml:"v"`
				Inline  richText `xml:"is"`
				Formula *string  `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := read(sheetPath, &sheet); err != nil {
		return nil, fmt.Errorf("ошибка чтения листа: %w", err)
	}
	out := [][]string{}
	for _, row := range sheet.Rows {
		n := row.Number
		if n == 0 {
			n = len(out) + 1
		}
		if n <= len(out) || n > 10001 {
			return nil, fmt.Errorf("неверный номер строки или больше 10000 строк данных")
		}
		values := []string{}
		used := map[int]bool{}
		for i, c := range row.Cells {
			col := i
			if c.Ref != "" {
				col = 0
				pos := 0
				for pos < len(c.Ref) && c.Ref[pos] >= 'A' && c.Ref[pos] <= 'Z' {
					col = col*26 + int(c.Ref[pos]-'A') + 1
					pos++
					if col > 128 {
						break
					}
				}
				rr, e := strconv.Atoi(c.Ref[pos:])
				if e != nil || rr != n || col == 0 {
					return nil, fmt.Errorf("неверный адрес ячейки %s", c.Ref)
				}
				col--
			}
			if col < 0 || col >= 128 || used[col] {
				return nil, fmt.Errorf("некорректные или лишние столбцы")
			}
			used[col] = true
			if c.Formula != nil {
				return nil, fmt.Errorf("строка %d: замените формулы значениями перед импортом", n)
			}
			s := c.Value
			switch c.Type {
			case "s":
				index, e := strconv.Atoi(s)
				if e != nil || index < 0 || index >= len(shared) {
					return nil, fmt.Errorf("неверная ссылка на строку")
				}
				s = shared[index]
			case "inlineStr":
				s = value(c.Inline)
			case "e":
				return nil, fmt.Errorf("строка %d содержит ошибку Excel", n)
			case "", "n", "str", "b":
			default:
				return nil, fmt.Errorf("неподдерживаемый тип ячейки %s", c.Type)
			}
			if len([]rune(s)) > 2000 {
				return nil, fmt.Errorf("строка %d: слишком длинное значение", n)
			}
			for len(values) <= col {
				values = append(values, "")
			}
			values[col] = strings.TrimSpace(s)
		}
		for len(out) < n-1 {
			out = append(out, nil)
		}
		out = append(out, values)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("первый лист пуст")
	}
	return out, nil
}
