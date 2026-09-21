// Package domain contains OKZ catalog values and validation rules.
package domain

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type Occupation struct {
	Code       string `json:"code"`
	Level      int    `json:"level"`
	ParentCode string `json:"parent_code,omitempty"`
	Name       string `json:"name"`
}

type Version struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	SourceName  string `json:"source_name"`
	SourceURL   string `json:"source_url,omitempty"`
	EffectiveOn string `json:"effective_on"`
	Status      string `json:"status"`
	ItemCount   int    `json:"item_count"`
	ImportedAt  string `json:"imported_at"`
	ActivatedAt string `json:"activated_at"`
}

type SearchPage struct {
	Items   []Occupation `json:"items"`
	Total   int          `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	Version string       `json:"version,omitempty"`
}

type Import struct {
	Version     string
	SourceName  string
	SourceURL   string
	EffectiveOn string
	ImportedBy  string
	Records     []Occupation
}

func ParseCSV(reader io.Reader) ([]Occupation, error) {
	const maxCSVBytes = 5 << 20
	data, err := io.ReadAll(io.LimitReader(reader, maxCSVBytes+1))
	if err != nil {
		return nil, fmt.Errorf("прочитать CSV: %w", err)
	}
	if len(data) > maxCSVBytes {
		return nil, fmt.Errorf("CSV превышает 5 МБ")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	firstLine := text
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		firstLine = text[:index]
	}
	delimiter := ','
	if strings.Count(firstLine, ";") > strings.Count(firstLine, ",") {
		delimiter = ';'
	}
	parser := csv.NewReader(strings.NewReader(text))
	parser.Comma = delimiter
	parser.FieldsPerRecord = -1
	parser.TrimLeadingSpace = true
	rows, err := parser.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("некорректный CSV: %w", err)
	}
	if len(rows) < 2 || len(rows[0]) < 2 {
		return nil, fmt.Errorf("CSV должен содержать заголовок code,name и хотя бы одну строку")
	}
	codeHeader := strings.ToLower(strings.TrimSpace(rows[0][0]))
	nameHeader := strings.ToLower(strings.TrimSpace(rows[0][1]))
	if (codeHeader != "code" && codeHeader != "код") ||
		(nameHeader != "name" && nameHeader != "наименование") {
		return nil, fmt.Errorf("первые колонки CSV должны называться code,name или код,наименование")
	}
	records := make([]Occupation, 0, len(rows)-1)
	for index, row := range rows[1:] {
		if len(row) < 2 {
			return nil, fmt.Errorf("строка %d: нужны код и наименование", index+2)
		}
		code := strings.TrimSpace(row[0])
		name := strings.Join(strings.Fields(row[1]), " ")
		if code == "" && name == "" {
			continue
		}
		records = append(records, Occupation{Code: code, Name: name})
	}
	return Normalize(records)
}

func Normalize(records []Occupation) ([]Occupation, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("справочник ОКЗ не содержит записей")
	}
	byCode := make(map[string]Occupation, len(records))
	for index, record := range records {
		record.Code = strings.TrimSpace(record.Code)
		record.Name = strings.Join(strings.Fields(record.Name), " ")
		if !ValidCode(record.Code) {
			return nil, fmt.Errorf("строка %d: код ОКЗ должен содержать от 1 до 4 цифр", index+2)
		}
		if length := utf8.RuneCountInString(record.Name); length < 2 || length > 500 {
			return nil, fmt.Errorf("строка %d: наименование должно содержать от 2 до 500 символов", index+2)
		}
		if _, exists := byCode[record.Code]; exists {
			return nil, fmt.Errorf("строка %d: код ОКЗ %s повторяется", index+2, record.Code)
		}
		record.Level = len(record.Code)
		if record.Level > 1 {
			record.ParentCode = record.Code[:record.Level-1]
		}
		byCode[record.Code] = record
	}
	for _, record := range byCode {
		if record.ParentCode == "" {
			continue
		}
		if _, exists := byCode[record.ParentCode]; !exists {
			return nil, fmt.Errorf("для кода ОКЗ %s отсутствует родительская группа %s", record.Code, record.ParentCode)
		}
	}
	normalized := make([]Occupation, 0, len(byCode))
	for _, record := range byCode {
		normalized = append(normalized, record)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Code < normalized[j].Code })
	return normalized, nil
}

func ValidCode(code string) bool {
	if len(code) < 1 || len(code) > 4 {
		return false
	}
	for _, symbol := range code {
		if symbol < '0' || symbol > '9' {
			return false
		}
	}
	return true
}
