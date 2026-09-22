package normativehttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/modules/normative/domain"
)

type serviceStub struct {
	imported domain.Import
}

func (s *serviceStub) List(context.Context, string) ([]domain.Source, error) {
	return []domain.Source{}, nil
}

func (s *serviceStub) Import(_ context.Context, input domain.Import) (domain.Source, *domain.DiffProtocol, error) {
	s.imported = input
	return domain.Source{ActCode: input.ActCode, Revision: input.Revision}, nil, nil
}

func (s *serviceStub) Diff(context.Context, string, string, string) (domain.DiffProtocol, error) {
	return domain.DiffProtocol{Changed: true}, nil
}

func TestImportParsesOfficialFileMetadataAndActor(t *testing.T) {
	content := []byte("%PDF-1.7 official")
	digest := sha256.Sum256(content)
	fields := map[string]string{
		"act_code":     "ORDER-270",
		"title":        "Приказ Минцифры № 270",
		"revision":     "2026.1",
		"published_on": "2026-03-20",
		"effective_on": "2026-04-01",
		"source_url":   "https://publication.pravo.gov.ru/document/0001202603200024",
		"sha256":       hex.EncodeToString(digest[:]),
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := writer.CreateFormFile("file", "order.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	service := &serviceStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/admin/normative-sources/import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	New(service).Import(response, request, "actor-id")

	if response.Code != http.StatusCreated || service.imported.ImportedBy != "actor-id" || service.imported.OriginalFilename != "order.pdf" || !bytes.Equal(service.imported.Content, content) {
		t.Fatalf("status=%d body=%q import=%#v", response.Code, response.Body.String(), service.imported)
	}
}

func TestDiffReturnsProtocol(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/normative-sources/diff?act_code=ORDER-270&from_revision=1&to_revision=2", nil)
	New(&serviceStub{}).Diff(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"changed":true`) {
		t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
	}
}
