// Package okzhttp adapts OKZ use cases to net/http.
package okzhttp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cybercalc/internal/modules/okz/domain"
	"cybercalc/internal/platform/apperror"
	"cybercalc/internal/platform/httpx"
)

const maxImportRequestBytes = 6 << 20

type Service interface {
	Search(context.Context, string, int, int, int) (domain.SearchPage, error)
	Versions(context.Context) ([]domain.Version, error)
	Import(context.Context, domain.Import) (domain.Version, error)
}

type Handler struct {
	service Service
}

func New(service Service) *Handler { return &Handler{service: service} }

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	level, err := queryInteger(r, "level")
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	limit, err := queryInteger(r, "limit")
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	offset, err := queryInteger(r, "offset")
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	page, err := h.service.Search(r.Context(), r.URL.Query().Get("q"), level, limit, offset)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.service.Versions(r.Context())
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, versions)
}

func (h *Handler) Import(w http.ResponseWriter, r *http.Request, actorID string) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportRequestBytes)
	if err := r.ParseMultipartForm(maxImportRequestBytes); err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "invalid_okz_upload", "не удалось прочитать форму или файл превышает 5 МБ", nil, err))
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "okz_file_required", "выберите CSV-файл ОКЗ", map[string]string{"file": "обязательное поле"}, err))
		return
	}
	defer file.Close()
	records, err := domain.ParseCSV(file)
	if err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "invalid_okz_csv", err.Error(), map[string]string{"file": err.Error()}, err))
		return
	}
	version, err := h.service.Import(r.Context(), domain.Import{
		Version: r.FormValue("version"), SourceName: r.FormValue("source_name"), SourceURL: r.FormValue("source_url"),
		EffectiveOn: r.FormValue("effective_on"), ImportedBy: actorID, Records: records,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, version)
}

func (h *Handler) Template(w http.ResponseWriter, _ *http.Request) {
	content := "\ufeffcode;name\r\n" +
		"2;Специалисты высшего уровня квалификации\r\n" +
		"25;Специалисты по информационно-коммуникационным технологиям\r\n" +
		"251;Разработчики и аналитики программного обеспечения и приложений\r\n" +
		"2512;Разработчики программного обеспечения\r\n"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="okz-import-template.csv"`)
	w.Header().Set("Content-Length", strconv.Itoa(len([]byte(content))))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(content))
}

func queryInteger(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, apperror.New(apperror.KindValidation, "invalid_query", fmt.Sprintf("параметр %s должен быть целым числом", name), map[string]string{name: "ожидалось целое число"}, err)
	}
	return value, nil
}
