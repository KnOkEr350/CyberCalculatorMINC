package okzhttp

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/modules/okz/domain"
)

type serviceStub struct {
	searchPage domain.SearchPage
	imported   domain.Import
}

func (s *serviceStub) Search(context.Context, string, int, int, int) (domain.SearchPage, error) {
	return s.searchPage, nil
}
func (s *serviceStub) Versions(context.Context) ([]domain.Version, error) {
	return []domain.Version{}, nil
}
func (s *serviceStub) Import(_ context.Context, input domain.Import) (domain.Version, error) {
	s.imported = input
	return domain.Version{Version: input.Version, ItemCount: len(input.Records)}, nil
}

func TestSearchReturnsEnvelope(t *testing.T) {
	handler := New(&serviceStub{searchPage: domain.SearchPage{Items: []domain.Occupation{}, Limit: 50, Version: "2026.1"}})
	response := httptest.NewRecorder()
	handler.Search(response, httptest.NewRequest(http.MethodGet, "/api/okz?q=developer", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"2026.1"`) || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestImportParsesCSVAndActor(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range map[string]string{"version": "2026.1", "source_name": "Росстандарт", "effective_on": "2026-01-01"} {
		_ = writer.WriteField(key, value)
	}
	file, err := writer.CreateFormFile("file", "okz.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("code,name\n2,Специалисты\n25,ИКТ специалисты\n"))
	_ = writer.Close()
	service := &serviceStub{}
	handler := New(service)
	request := httptest.NewRequest(http.MethodPost, "/api/admin/okz/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	handler.Import(response, request, "actor-id")
	if response.Code != http.StatusCreated || service.imported.ImportedBy != "actor-id" || len(service.imported.Records) != 2 {
		t.Fatalf("status=%d body=%q import=%#v", response.Code, response.Body.String(), service.imported)
	}
}

func TestTemplateIsDownloadableCSV(t *testing.T) {
	response := httptest.NewRecorder()
	New(&serviceStub{}).Template(response, httptest.NewRequest(http.MethodGet, "/api/admin/okz/template", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Disposition"), "okz-import-template.csv") || !strings.Contains(response.Body.String(), "2512") {
		t.Fatalf("headers=%v body=%q", response.Header(), response.Body.String())
	}
}
