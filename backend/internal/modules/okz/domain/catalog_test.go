package domain

import (
	"strings"
	"testing"
)

func TestParseCSVNormalizesHierarchy(t *testing.T) {
	records, err := ParseCSV(strings.NewReader("код;наименование\n2;Специалисты высшего уровня квалификации\n25;Специалисты по информационно-коммуникационным технологиям\n251;Разработчики и аналитики программного обеспечения\n2512;Разработчики программного обеспечения\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 || records[3].Code != "2512" || records[3].ParentCode != "251" || records[3].Level != 4 {
		t.Fatalf("unexpected records: %#v", records)
	}
}

func TestParseCSVRejectsInvalidCatalog(t *testing.T) {
	tests := []string{
		"code,name\n2512,Разработчики\n",
		"code,name\n2,Специалисты\n2,Повтор\n",
		"code,name\n2,Специалисты\n25x,Ошибка\n",
	}
	for _, input := range tests {
		if _, err := ParseCSV(strings.NewReader(input)); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestParseCSVRejectsInvalidUTF8(t *testing.T) {
	_, err := ParseCSV(strings.NewReader("code;name\n2;\xff\n"))
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected UTF-8 validation error, got %v", err)
	}
}
