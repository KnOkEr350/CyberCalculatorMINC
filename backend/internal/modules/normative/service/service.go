// Package service implements normative source registry use cases.
package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/modules/normative/domain"
	"cybercalc/internal/modules/normative/repository"
	"cybercalc/internal/platform/apperror"
)

const MaxContentBytes = 25 << 20

var actCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{1,63}$`)
var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Service struct{ registry repository.Registry }

func New(registry repository.Registry) *Service { return &Service{registry: registry} }

func (s *Service) List(ctx context.Context, actCode string) ([]domain.Source, error) {
	actCode = strings.ToUpper(strings.TrimSpace(actCode))
	if actCode != "" && !actCodePattern.MatchString(actCode) {
		return nil, validation("act_code", "некорректный код нормативного акта")
	}
	items, err := s.registry.List(ctx, actCode)
	if err != nil {
		return nil, internal("не удалось загрузить нормативные источники", err)
	}
	return items, nil
}

func (s *Service) Import(ctx context.Context, input domain.Import) (domain.Source, *domain.DiffProtocol, error) {
	input.ActCode = strings.ToUpper(strings.TrimSpace(input.ActCode))
	input.Title = strings.Join(strings.Fields(input.Title), " ")
	input.Revision = strings.Join(strings.Fields(input.Revision), " ")
	input.PublishedOn = strings.TrimSpace(input.PublishedOn)
	input.EffectiveOn = strings.TrimSpace(input.EffectiveOn)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.ExpectedSHA256 = strings.ToLower(strings.TrimSpace(input.ExpectedSHA256))
	input.ContentType = strings.ToLower(strings.TrimSpace(strings.Split(input.ContentType, ";")[0]))
	input.OriginalFilename = filepath.Base(strings.ReplaceAll(strings.TrimSpace(input.OriginalFilename), `\`, "/"))
	if !actCodePattern.MatchString(input.ActCode) {
		return domain.Source{}, nil, validation("act_code", "код должен содержать 2–64 символа A-Z, 0-9, точку, дефис или подчёркивание")
	}
	if length := utf8.RuneCountInString(input.Title); length < 3 || length > 500 {
		return domain.Source{}, nil, validation("title", "название акта должно содержать от 3 до 500 символов")
	}
	if length := utf8.RuneCountInString(input.Revision); length < 1 || length > 100 {
		return domain.Source{}, nil, validation("revision", "редакция должна содержать от 1 до 100 символов")
	}
	if input.ImportedBy == "" {
		return domain.Source{}, nil, apperror.New(apperror.KindUnauthorized, "actor_required", "требуется авторизация", nil, nil)
	}
	if len(input.Content) == 0 || len(input.Content) > MaxContentBytes {
		return domain.Source{}, nil, validation("file", "официальный файл должен иметь размер от 1 байта до 25 МБ")
	}
	if !shaPattern.MatchString(input.ExpectedSHA256) {
		return domain.Source{}, nil, validation("sha256", "укажите SHA-256 из 64 шестнадцатеричных символов")
	}
	digest := sha256.Sum256(input.Content)
	actual := hex.EncodeToString(digest[:])
	if actual != input.ExpectedSHA256 {
		return domain.Source{}, nil, validation("sha256", fmt.Sprintf("SHA-256 файла не совпадает; вычислено %s", actual))
	}
	parsed, err := url.Parse(input.SourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return domain.Source{}, nil, validation("source_url", "нужна HTTPS-ссылка официального источника без credentials, fragment и нестандартного порта")
	}
	input.SourceHost = strings.ToLower(parsed.Hostname())
	parsed.Host = input.SourceHost
	input.SourceURL = parsed.String()
	trusted, err := s.registry.IsTrustedHost(ctx, input.SourceHost)
	if err != nil {
		return domain.Source{}, nil, internal("не удалось проверить официальный источник", err)
	}
	if !trusted {
		return domain.Source{}, nil, apperror.New(apperror.KindForbidden, "untrusted_normative_host", "домен источника не входит в реестр официальных", map[string]string{"source_url": "используйте утверждённый официальный домен"}, nil)
	}
	if _, err = time.Parse("2006-01-02", input.EffectiveOn); err != nil {
		return domain.Source{}, nil, validation("effective_on", "дата действия должна иметь формат YYYY-MM-DD")
	}
	if input.PublishedOn != "" {
		if _, err = time.Parse("2006-01-02", input.PublishedOn); err != nil {
			return domain.Source{}, nil, validation("published_on", "дата публикации должна иметь формат YYYY-MM-DD")
		}
	}
	if input.OriginalFilename == "" || len(input.OriginalFilename) > 255 {
		return domain.Source{}, nil, validation("file", "имя файла обязательно и не должно превышать 255 символов")
	}
	if !supportedContent(input.ContentType, input.Content) {
		return domain.Source{}, nil, validation("file", "поддерживаются PDF, DOCX, XLSX, JSON, CSV и UTF-8 text")
	}
	source, diff, err := s.registry.Import(ctx, input)
	if errors.Is(err, repository.ErrRevisionConflict) {
		return domain.Source{}, nil, apperror.New(apperror.KindConflict, "normative_revision_conflict", "эта редакция уже зарегистрирована с другими реквизитами или SHA-256", map[string]string{"revision": "уточните редакцию или официальный файл"}, err)
	}
	if errors.Is(err, repository.ErrEffectiveDateConflict) {
		return domain.Source{}, nil, apperror.New(apperror.KindConflict, "normative_effective_date_conflict", "на эту дату уже зарегистрирована другая редакция акта", map[string]string{"effective_on": "даты действия редакций не должны конфликтовать"}, err)
	}
	if err != nil {
		return domain.Source{}, nil, internal("не удалось зарегистрировать нормативный источник", err)
	}
	return source, diff, nil
}

func (s *Service) Diff(ctx context.Context, actCode, fromRevision, toRevision string) (domain.DiffProtocol, error) {
	actCode = strings.ToUpper(strings.TrimSpace(actCode))
	fromRevision = strings.TrimSpace(fromRevision)
	toRevision = strings.TrimSpace(toRevision)
	if !actCodePattern.MatchString(actCode) || fromRevision == "" || toRevision == "" || fromRevision == toRevision {
		return domain.DiffProtocol{}, validation("revision", "укажите акт и две разные редакции")
	}
	from, err := s.registry.Find(ctx, actCode, fromRevision)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.DiffProtocol{}, notFound()
	}
	if err != nil {
		return domain.DiffProtocol{}, internal("не удалось загрузить исходную редакцию", err)
	}
	to, err := s.registry.Find(ctx, actCode, toRevision)
	if errors.Is(err, repository.ErrNotFound) {
		return domain.DiffProtocol{}, notFound()
	}
	if err != nil {
		return domain.DiffProtocol{}, internal("не удалось загрузить новую редакцию", err)
	}
	return domain.Compare(from, to), nil
}

func supportedContent(contentType string, content []byte) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "application/pdf" {
		return len(content) >= 4 && string(content[:4]) == "%PDF"
	}
	if contentType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || contentType == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		return validOpenXML(contentType, content)
	}
	if contentType == "application/json" {
		return json.Valid(content)
	}
	if contentType == "text/plain" || contentType == "text/csv" {
		return utf8.Valid(content) && !strings.ContainsRune(string(content), '\x00')
	}
	return false
}

func validOpenXML(contentType string, content []byte) bool {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return false
	}
	wantPrefix := "word/"
	if strings.Contains(contentType, "spreadsheetml") {
		wantPrefix = "xl/"
	}
	hasContentTypes, hasPayload := false, false
	for _, file := range archive.File {
		if file.Name == "[Content_Types].xml" {
			hasContentTypes = true
		}
		if strings.HasPrefix(file.Name, wantPrefix) {
			hasPayload = true
		}
	}
	return hasContentTypes && hasPayload
}
func validation(field, message string) error {
	return apperror.New(apperror.KindValidation, "invalid_normative_source", message, map[string]string{field: message}, nil)
}
func notFound() error {
	return apperror.New(apperror.KindNotFound, "normative_revision_not_found", "редакция нормативного акта не найдена", nil, nil)
}
func internal(message string, cause error) error {
	return apperror.New(apperror.KindInternal, "normative_registry_error", message, nil, cause)
}
