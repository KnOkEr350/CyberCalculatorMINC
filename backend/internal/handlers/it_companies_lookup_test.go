package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestITLookupParsesOnlyAccreditedCompanies(t *testing.T) {
	article := `<article><h3>ООО &quot;КИБЕРПРОТЕКТ&quot;</h3><span>Аккредитована</span><p>ИНН <!-- -->9715274292</p></article>`
	body := article + article + `<article><h3>Исключённая</h3><span>Исключена</span><p>ИНН 7704784467</p></article>`
	items, err := parseITLookup(body, "Киберпротект", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	if err != nil || len(items) != 1 || items[0].INN != "9715274292" || items[0].Name != `ООО "КИБЕРПРОТЕКТ"` || items[0].OGRN != "" || !strings.Contains(items[0].SourceURL, "proreestr.ru/") {
		t.Fatalf("incorrect registry result: %v %v", items, err)
	}
}

func TestITLookupDistinguishesNoMatchesFromSourceFailure(t *testing.T) {
	items, err := parseITLookup(`<p>По запросу аккредитация не найдена</p>`, "test", time.Now())
	if err != nil || len(items) != 0 {
		t.Fatalf("empty search: %v %v", items, err)
	}
	for _, body := range []string{`<h1>Service unavailable</h1>`, `<article><h3>Bad INN</h3><span>Аккредитована</span><p>ИНН 9715274291</p></article>`} {
		if _, err := parseITLookup(body, "test", time.Now()); err == nil {
			t.Fatal("source failure mistaken for no matches")
		}
	}
}
