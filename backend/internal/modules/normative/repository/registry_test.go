package repository

import (
	"testing"

	"cybercalc/internal/modules/normative/domain"
)

func TestSameImportRequiresAllImmutableMetadata(t *testing.T) {
	input := domain.Import{
		ActCode: "ORDER-270", Title: "Order", Revision: "1", PublishedOn: "2026-01-01",
		EffectiveOn: "2026-02-01", SourceURL: "https://pravo.gov.ru/order", SourceHost: "pravo.gov.ru",
		ExpectedSHA256: "hash", ContentType: "application/pdf", OriginalFilename: "order.pdf", ImportedBy: "actor",
	}
	existing := domain.Source{
		ActCode: input.ActCode, Title: input.Title, Revision: input.Revision, PublishedOn: input.PublishedOn,
		EffectiveOn: input.EffectiveOn, SourceURL: input.SourceURL, SourceHost: input.SourceHost,
		ContentSHA256: input.ExpectedSHA256, ContentType: input.ContentType, OriginalFilename: input.OriginalFilename, ImportedBy: input.ImportedBy,
	}
	if !sameImport(existing, input) {
		t.Fatal("identical import must be idempotent")
	}
	input.ImportedBy = "another-actor"
	if !sameImport(existing, input) {
		t.Fatal("retry by another administrator must remain idempotent")
	}
	input.EffectiveOn = "2026-03-01"
	if sameImport(existing, input) {
		t.Fatal("conflicting effective date must not be idempotent")
	}
}
