// Package service implements OKZ catalog use cases.
package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/modules/okz/domain"
	"cybercalc/internal/modules/okz/repository"
	"cybercalc/internal/platform/apperror"
)

const (
	defaultLimit = 50
	maxLimit     = 100
	maxOffset    = 1_000_000
)

var versionPattern = regexp.MustCompile(`^[0-9A-Za-zА-Яа-яЁё][0-9A-Za-zА-Яа-яЁё._ /()-]{0,63}$`)

type Service struct {
	catalog repository.Catalog
}

func New(catalog repository.Catalog) *Service { return &Service{catalog: catalog} }

func (s *Service) Search(ctx context.Context, query string, level, limit, offset int) (domain.SearchPage, error) {
	query = strings.Join(strings.Fields(query), " ")
	if utf8.RuneCountInString(query) > 200 {
		return domain.SearchPage{}, validation("query", "поисковая строка не должна превышать 200 символов")
	}
	if level < 0 || level > 4 {
		return domain.SearchPage{}, validation("level", "уровень ОКЗ должен быть от 1 до 4")
	}
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return domain.SearchPage{}, validation("limit", "limit должен быть от 1 до 100")
	}
	if offset < 0 || offset > maxOffset {
		return domain.SearchPage{}, validation("offset", "offset должен быть от 0 до 1000000")
	}
	page, err := s.catalog.Search(ctx, query, level, limit, offset)
	if errors.Is(err, repository.ErrNoActiveVersion) {
		if page.Items == nil {
			page.Items = []domain.Occupation{}
		}
		page.Limit = limit
		page.Offset = offset
		return page, nil
	}
	if err != nil {
		return domain.SearchPage{}, internal("не удалось найти записи ОКЗ", err)
	}
	return page, nil
}

func (s *Service) Versions(ctx context.Context) ([]domain.Version, error) {
	versions, err := s.catalog.ListVersions(ctx)
	if err != nil {
		return nil, internal("не удалось получить версии ОКЗ", err)
	}
	return versions, nil
}

func (s *Service) Import(ctx context.Context, input domain.Import) (domain.Version, error) {
	input.Version = strings.Join(strings.Fields(input.Version), " ")
	input.SourceName = strings.Join(strings.Fields(input.SourceName), " ")
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.EffectiveOn = strings.TrimSpace(input.EffectiveOn)
	if !versionPattern.MatchString(input.Version) {
		return domain.Version{}, validation("version", "версия должна содержать от 1 до 64 допустимых символов")
	}
	if length := utf8.RuneCountInString(input.SourceName); length < 2 || length > 300 {
		return domain.Version{}, validation("source_name", "название источника должно содержать от 2 до 300 символов")
	}
	if input.SourceURL != "" {
		parsed, err := url.Parse(input.SourceURL)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return domain.Version{}, validation("source_url", "ссылка на источник должна быть корректным HTTPS-адресом")
		}
	}
	if _, err := time.Parse("2006-01-02", input.EffectiveOn); err != nil {
		return domain.Version{}, validation("effective_on", "дата действия должна иметь формат YYYY-MM-DD")
	}
	if input.ImportedBy == "" {
		return domain.Version{}, apperror.New(apperror.KindUnauthorized, "actor_required", "требуется авторизация", nil, nil)
	}
	records, err := domain.Normalize(input.Records)
	if err != nil {
		return domain.Version{}, validation("file", err.Error())
	}
	input.Records = records
	version, err := s.catalog.Import(ctx, input)
	if errors.Is(err, repository.ErrVersionExists) {
		return domain.Version{}, apperror.New(apperror.KindConflict, "okz_version_exists", "версия ОКЗ с таким названием уже загружена", map[string]string{"version": "выберите новое обозначение версии"}, err)
	}
	if err != nil {
		return domain.Version{}, internal("не удалось импортировать справочник ОКЗ", err)
	}
	return version, nil
}

func validation(field, message string) error {
	return apperror.New(apperror.KindValidation, "invalid_okz_catalog", message, map[string]string{field: message}, nil)
}

func internal(message string, cause error) error {
	return apperror.New(apperror.KindInternal, "okz_storage_error", message, nil, cause)
}
