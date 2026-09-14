package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEGRULClientReadsCandidatesInReturnedOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method %s", r.Method)
			}
			fmt.Fprint(w, `{"t":"ticket","captchaRequired":false}`)
		case "/search-result/ticket":
			fmt.Fprint(w, `{"rows":[{"i":"7707083893","o":"1027700132195","n":"Первый","rn":"Москва"},{"i":"1650084264","o":"1021602020384","n":"Второй","rn":"Татарстан"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := egrulClient{baseURL: server.URL, http: &http.Client{Timeout: time.Second}}
	result, err := client.search(context.Background(), "Тестовый вуз")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[0].INN != "7707083893" || result.Rows[0].Name != "Первый" {
		t.Fatalf("candidate order changed: %#v", result.Rows)
	}
}

func TestEGRULClientStopsOnCaptcha(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"captchaRequired":true}`)
	}))
	defer server.Close()
	client := egrulClient{baseURL: server.URL, http: server.Client()}
	if _, err := client.search(context.Background(), "Тестовый вуз"); err == nil {
		t.Fatal("CAPTCHA response accepted")
	}
}

func TestEGRULClientWaitsForAsyncResult(t *testing.T) {
	resultRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `{"t":"ticket","captchaRequired":false}`)
		case "/search-result/ticket":
			resultRequests++
			if resultRequests < 2 {
				fmt.Fprint(w, `{"rows":[]}`)
				return
			}
			fmt.Fprint(w, `{"rows":[{"i":"7707083893","o":"1027700132195","n":"Тестовый вуз","rn":"Москва"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := egrulClient{baseURL: server.URL, http: server.Client()}
	result, err := client.search(context.Background(), "Тестовый вуз")
	if err != nil {
		t.Fatal(err)
	}
	if resultRequests != 2 || len(result.Rows) != 1 {
		t.Fatalf("async result was not awaited: requests=%d result=%#v", resultRequests, result)
	}
}

func TestSelectEGRULCandidateUsesOrganizationName(t *testing.T) {
	rows := []egrulSearchRow{
		{INN: "7707083893", OGRN: "1027700132195", Name: "Совершенно другая организация", Region: "Москва"},
		{INN: "1650084264", OGRN: "1021602020384", Name: "Российский экономический университет имени Г. В. Плеханова", Region: "Москва"},
	}
	match, ok := selectEGRULCandidate(rows,
		"Брянский филиал федерального государственного бюджетного образовательного учреждения высшего образования «Российский экономический университет имени Г.В. Плеханова»",
		"Брянская область")
	if !ok || match.INN != "1650084264" {
		t.Fatalf("wrong candidate selected: ok=%v match=%#v", ok, match)
	}
}

func TestSelectEGRULCandidateRejectsUnrelatedResult(t *testing.T) {
	rows := []egrulSearchRow{{INN: "7707083893", OGRN: "1027700132195", Name: "Совершенно другая организация", Region: "Москва"}}
	if match, ok := selectEGRULCandidate(rows, "Ярославский государственный университет имени П. Г. Демидова", "Ярославская область"); ok {
		t.Fatalf("unrelated candidate accepted: %#v", match)
	}
}

func TestSelectEGRULCandidateRejectsGenericWordOverlap(t *testing.T) {
	tests := []struct {
		name      string
		queryName string
		rowName   string
	}{
		{
			name:      "generic institute words",
			queryName: "Смоленский казачий институт промышленных технологий и бизнеса (филиал) Московского государственного университета технологий и управления имени К.Г. Разумовского",
			rowName:   "Частное образовательное учреждение высшего образования Институт управления, бизнеса и технологий",
		},
		{
			name:      "university support fund",
			queryName: "Филиал Самарского государственного технического университета в г. Новокуйбышевске",
			rowName:   "Фонд развития Самарского государственного технического унисерситета",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := []egrulSearchRow{{INN: "7707083893", OGRN: "1027700132195", Name: test.rowName, Region: "Москва"}}
			if match, ok := selectEGRULCandidate(rows, test.queryName, "Смоленская область"); ok {
				t.Fatalf("generic overlap accepted: %#v", match)
			}
		})
	}
}

func TestSelectEGRULCandidateRejectsSingleGenericNameToken(t *testing.T) {
	rows := []egrulSearchRow{{
		INN: "7707083893", OGRN: "1027700132195",
		Name:   "Федеральное государственное бюджетное учреждение Российская академия образования",
		Region: "Москва",
	}}
	if match, ok := selectEGRULCandidate(rows, "Российская таможенная академия", "Московская область"); ok {
		t.Fatalf("single generic token accepted: %#v", match)
	}
}

func TestOrganizationSearchQueriesPreferQuotedParentName(t *testing.T) {
	queries := organizationSearchQueries(`Филиал ФГБОУ ВО "Алтайский государственный университет" в г. Бийске`)
	if len(queries) != 2 || queries[0] != "Алтайский государственный университет" {
		t.Fatalf("quoted parent name was not preferred: %#v", queries)
	}
}
