package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"cybercalc/internal/modules/normative/domain"
	"cybercalc/internal/modules/normative/repository"
	"cybercalc/internal/platform/apperror"
)

type registryStub struct {
	trusted   bool
	imported  domain.Import
	importErr error
	sources   map[string]domain.Source
}

func (r *registryStub) IsTrustedHost(context.Context, string) (bool, error) {
	return r.trusted, nil
}

func (r *registryStub) List(context.Context, string) ([]domain.Source, error) {
	return []domain.Source{}, nil
}

func (r *registryStub) Import(_ context.Context, input domain.Import) (domain.Source, *domain.DiffProtocol, error) {
	r.imported = input
	return domain.Source{ActCode: input.ActCode, Revision: input.Revision}, nil, r.importErr
}

func (r *registryStub) Find(_ context.Context, _, revision string) (domain.Source, error) {
	source, ok := r.sources[revision]
	if !ok {
		return domain.Source{}, repository.ErrNotFound
	}
	return source, nil
}

func validImport() domain.Import {
	content := []byte("%PDF-1.7 official revision")
	digest := sha256.Sum256(content)
	return domain.Import{
		ActCode:          " order-270 ",
		Title:            " Приказ Минцифры № 270 ",
		Revision:         " 2026.1 ",
		PublishedOn:      "2026-03-20",
		EffectiveOn:      "2026-04-01",
		SourceURL:        "https://publication.pravo.gov.ru/document/0001202603200024",
		ExpectedSHA256:   hex.EncodeToString(digest[:]),
		ContentType:      "application/pdf",
		OriginalFilename: "order-270.pdf",
		ImportedBy:       "actor-id",
		Content:          content,
	}
}

func TestImportAcceptsVerifiedOfficialSource(t *testing.T) {
	repositoryStub := &registryStub{trusted: true}
	source, _, err := New(repositoryStub).Import(context.Background(), validImport())
	if err != nil {
		t.Fatal(err)
	}
	if source.ActCode != "ORDER-270" || repositoryStub.imported.SourceHost != "publication.pravo.gov.ru" || repositoryStub.imported.Title != "Приказ Минцифры № 270" {
		t.Fatalf("unexpected normalized import: %#v", repositoryStub.imported)
	}
}

func TestImportRejectsHashMismatchBeforeRepository(t *testing.T) {
	repositoryStub := &registryStub{trusted: true}
	input := validImport()
	input.ExpectedSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, _, err := New(repositoryStub).Import(context.Background(), input)
	assertKind(t, err, apperror.KindValidation)
	if repositoryStub.imported.ActCode != "" {
		t.Fatal("invalid content reached repository")
	}
}

func TestImportRejectsUnofficialHost(t *testing.T) {
	repositoryStub := &registryStub{trusted: false}
	input := validImport()
	input.SourceURL = "https://example.org/order.pdf"
	_, _, err := New(repositoryStub).Import(context.Background(), input)
	assertKind(t, err, apperror.KindForbidden)
}

func TestSupportedContentRejectsSpoofedFormats(t *testing.T) {
	if supportedContent("application/json", []byte("not json")) {
		t.Fatal("invalid JSON must be rejected")
	}
	if supportedContent("application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("PK fake zip")) {
		t.Fatal("spoofed DOCX must be rejected")
	}
}

func TestImportMapsContradictoryRevision(t *testing.T) {
	repositoryStub := &registryStub{trusted: true, importErr: repository.ErrRevisionConflict}
	_, _, err := New(repositoryStub).Import(context.Background(), validImport())
	assertKind(t, err, apperror.KindConflict)
}

func TestDiffBuildsProtocolFromRegisteredRevisions(t *testing.T) {
	repositoryStub := &registryStub{sources: map[string]domain.Source{
		"1": {ID: "old", ActCode: "ORDER-270", Revision: "1", ContentSHA256: "old-hash", Content: []byte("old")},
		"2": {ID: "new", ActCode: "ORDER-270", Revision: "2", ContentSHA256: "new-hash", Content: []byte("new")},
	}}
	protocol, err := New(repositoryStub).Diff(context.Background(), "order-270", "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.Changed || protocol.FromID != "old" || protocol.ToID != "new" {
		t.Fatalf("unexpected protocol: %#v", protocol)
	}
}

func assertKind(t *testing.T, err error, want apperror.Kind) {
	t.Helper()
	var applicationError *apperror.Error
	if !errors.As(err, &applicationError) || applicationError.Kind != want {
		t.Fatalf("unexpected error: %#v", err)
	}
}
