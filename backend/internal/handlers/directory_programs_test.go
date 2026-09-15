package handlers

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestProgramParserReadsEducationTablesFromNextPageData(t *testing.T) {
	table := `<table><tr itemprop="eduOp"><td><p itemprop="eduCode">09.03.01</p></td></tr></table>`
	data, _ := json.Marshal(map[string]interface{}{"props": map[string]interface{}{"pageProps": map[string]interface{}{"content": table, "example": "10.03.01"}}})
	page := `<script id="__NEXT_DATA__" type="application/json">` + string(data) + `</script><script>"09.04.04"</script>`
	if got := parseEducationPrograms(page); !reflect.DeepEqual(got, []string{"09.03.01"}) {
		t.Fatalf("wrong programs from page data: %v", got)
	}
}

func TestProgramParserIgnoresGroupsAndUnrelatedPageText(t *testing.T) {
	page := `<script>09.03.04</script><nav>09.04.04</nav><table>
	<tr><td>09.00.00</td><td>Информатика</td></tr>
	<tr><td itemprop="eduCode"><span>09.03.01</span></td><td>Информатика и вычислительная техника</td></tr>
	<tr><td>38.03.05</td><td>Бизнес-информатика</td></tr>
	<tr><td>09.03.01</td><td>Очная форма</td></tr>
	<tr><td>Ранее действовала программа 10.03.01</td></tr></table>`
	if got := parseEducationPrograms(page); !reflect.DeepEqual(got, []string{"09.03.01", "38.03.05"}) {
		t.Fatalf("wrong programs: %v", got)
	}
}

func TestProgramCodesRejectBroadOrMalformedCodes(t *testing.T) {
	for _, value := range []string{"09.00.00", "09.03.00", "9.03.01", "09.03.01,10.03.01", "text"} {
		if _, err := normalizeProgramCodes([]string{value}); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestUniversityProgramURLKeepsBranchSite(t *testing.T) {
	got := universityProgramURL(`<table><tr><td>web-сайт</td><td>http://branch.example.edu/campus</td></tr></table>`)
	if got != "https://branch.example.edu/campus/sveden/education/" {
		t.Fatal(got)
	}
}

func TestUniversityProgramURLAcceptsWebsiteWithoutScheme(t *testing.T) {
	if got := universityProgramURL(`<table><tr><td>web-сайт</td><td>www.branch.example.edu</td></tr></table>`); got != "https://www.branch.example.edu/sveden/education/" {
		t.Fatal(got)
	}
}

func TestProgramParserReadsMarkedCodesOutsideTables(t *testing.T) {
	page := `<div itemprop="eduCode">09.03.01</div><p itemprop="eduCode">10.03.01</p><script><table><tr><td>38.03.05</td></tr></table></script>`
	if got := parseEducationPrograms(page); !reflect.DeepEqual(got, []string{"09.03.01", "10.03.01"}) {
		t.Fatalf("wrong marked programs: %v", got)
	}
}

func TestEducationProgramLinksStayOnInstitutionSite(t *testing.T) {
	page := `<a href="/sveden/education/programs/">Информация по образовательным программам</a>
	<a href="https://other.example/sveden/education/programs/">Образовательные программы</a>
	<a href="/sveden/education/admissions/">Результаты приема по образовательным программам</a>`
	if got := educationProgramLinks(page, "https://university.example/sveden/education/"); !reflect.DeepEqual(got, []string{"https://university.example/sveden/education/programs/"}) {
		t.Fatalf("wrong education links: %v", got)
	}
}
