package apicontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// QA-08: контракт описывает не только адреса, но и поведение — у каждой
// операции должен быть и успешный ответ, и описание ошибки. Инвентарь
// маршрутов сверяется в обе стороны в TestOpenAPIV1IsValidAndMatchesRegisteredRoutes.
func TestEveryOperationDeclaresSuccessAndError(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Paths) == 0 {
		t.Fatal("контракт не содержит ни одного пути")
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	operations := 0
	for path, item := range document.Paths {
		for method, body := range item {
			if !methods[strings.ToLower(method)] {
				continue // parameters и прочие поля пути
			}
			operations++
			var operation struct {
				Responses map[string]json.RawMessage `json:"responses"`
				// security: [] помечает публичную операцию. Такие эндпоинты
				// (health, features) отдают ответ всегда и ошибок не имеют.
				Security *[]json.RawMessage `json:"security"`
			}
			if err := json.Unmarshal(body, &operation); err != nil {
				t.Fatalf("%s %s: %v", strings.ToUpper(method), path, err)
			}
			if len(operation.Responses) == 0 {
				t.Fatalf("%s %s: операция без описанных ответов", strings.ToUpper(method), path)
			}
			success, failure := false, false
			for code := range operation.Responses {
				switch {
				case strings.HasPrefix(code, "2"):
					success = true
				// Ошибку описывает либо общий default, либо явный код 4xx/5xx —
				// так сделан, например, health с его 503.
				case code == "default", strings.HasPrefix(code, "4"), strings.HasPrefix(code, "5"):
					failure = true
				}
			}
			if !success {
				t.Fatalf("%s %s: не описан успешный ответ", strings.ToUpper(method), path)
			}
			public := operation.Security != nil && len(*operation.Security) == 0
			// Операция под аутентификацией всегда может ответить 401 или 403,
			// поэтому обязана описывать ошибку.
			if !public && !failure {
				t.Fatalf("%s %s: операция под аутентификацией не описывает ответ с ошибкой", strings.ToUpper(method), path)
			}
		}
	}
	if operations == 0 {
		t.Fatal("в контракте нет ни одной операции")
	}
}

// Каждая операция отнесена к разделу: без тега контракт невозможно читать и
// сопоставлять с модулями backend.
func TestEveryOperationIsTagged(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for path, item := range document.Paths {
		for method, body := range item {
			if !methods[strings.ToLower(method)] {
				continue
			}
			var operation struct {
				Tags []string `json:"tags"`
			}
			if err := json.Unmarshal(body, &operation); err != nil {
				t.Fatal(err)
			}
			if len(operation.Tags) == 0 {
				t.Fatalf("%s %s: операция без раздела", strings.ToUpper(method), path)
			}
		}
	}
}
