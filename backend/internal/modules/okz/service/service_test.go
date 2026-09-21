package service

import (
	"context"
	"errors"
	"testing"

	"cybercalc/internal/modules/okz/domain"
	"cybercalc/internal/modules/okz/repository"
	"cybercalc/internal/platform/apperror"
)

type catalogStub struct {
	searchInput struct {
		query                string
		level, limit, offset int
	}
	searchPage domain.SearchPage
	searchErr  error
	versions   []domain.Version
	imported   domain.Import
	importErr  error
}

func (c *catalogStub) Search(_ context.Context, query string, level, limit, offset int) (domain.SearchPage, error) {
	c.searchInput.query, c.searchInput.level, c.searchInput.limit, c.searchInput.offset = query, level, limit, offset
	return c.searchPage, c.searchErr
}
func (c *catalogStub) ListVersions(context.Context) ([]domain.Version, error) { return c.versions, nil }
func (c *catalogStub) Import(_ context.Context, input domain.Import) (domain.Version, error) {
	c.imported = input
	return domain.Version{Version: input.Version, ItemCount: len(input.Records)}, c.importErr
}

func TestSearchDefaultsAndNoActiveCatalog(t *testing.T) {
	repositoryStub := &catalogStub{searchErr: repository.ErrNoActiveVersion}
	page, err := New(repositoryStub).Search(context.Background(), "  разработчик  ", 4, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || repositoryStub.searchInput.query != "разработчик" || repositoryStub.searchInput.limit != 50 {
		t.Fatalf("unexpected page/input: %#v %#v", page, repositoryStub.searchInput)
	}
}

func TestImportValidatesAndNormalizes(t *testing.T) {
	repositoryStub := &catalogStub{}
	input := domain.Import{
		Version: "  2026.1  ", SourceName: " Росстандарт ", SourceURL: "https://protect.gost.ru/",
		EffectiveOn: "2026-01-01", ImportedBy: "actor",
		Records: []domain.Occupation{{Code: "2", Name: " Специалисты "}, {Code: "25", Name: " ИКТ специалисты "}},
	}
	version, err := New(repositoryStub).Import(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if version.ItemCount != 2 || repositoryStub.imported.Version != "2026.1" || repositoryStub.imported.Records[1].ParentCode != "2" {
		t.Fatalf("unexpected import: %#v", repositoryStub.imported)
	}
}

func TestImportMapsVersionConflict(t *testing.T) {
	repositoryStub := &catalogStub{importErr: repository.ErrVersionExists}
	_, err := New(repositoryStub).Import(context.Background(), domain.Import{
		Version: "2026.1", SourceName: "Росстандарт", EffectiveOn: "2026-01-01", ImportedBy: "actor",
		Records: []domain.Occupation{{Code: "2", Name: "Специалисты"}},
	})
	var applicationError *apperror.Error
	if !errors.As(err, &applicationError) || applicationError.Kind != apperror.KindConflict {
		t.Fatalf("unexpected error: %#v", err)
	}
}
