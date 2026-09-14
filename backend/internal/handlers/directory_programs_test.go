package handlers

import (
	"reflect"
	"testing"
)

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
