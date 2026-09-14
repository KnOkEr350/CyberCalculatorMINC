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
