// Package normativehttp adapts normative source use cases to net/http.
package normativehttp

import (
	"context"
	"io"
	"net/http"
	"strings"

	"cybercalc/internal/modules/normative/domain"
	"cybercalc/internal/modules/normative/service"
	"cybercalc/internal/platform/apperror"
	"cybercalc/internal/platform/httpx"
)

type Service interface {
	List(context.Context, string) ([]domain.Source, error)
	Import(context.Context, domain.Import) (domain.Source, *domain.DiffProtocol, error)
	Diff(context.Context, string, string, string) (domain.DiffProtocol, error)
}
type Handler struct{ service Service }

func New(service Service) *Handler { return &Handler{service: service} }

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.List(r.Context(), r.URL.Query().Get("act_code"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) Diff(w http.ResponseWriter, r *http.Request) {
	protocol, err := h.service.Diff(r.Context(), r.URL.Query().Get("act_code"), r.URL.Query().Get("from_revision"), r.URL.Query().Get("to_revision"))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, protocol)
}
func (h *Handler) Import(w http.ResponseWriter, r *http.Request, actorID string) {
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxContentBytes+(1<<20))
	if err := r.ParseMultipartForm(service.MaxContentBytes + (1 << 20)); err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "invalid_normative_upload", "форма повреждена или превышает 25 МБ", nil, err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "normative_file_required", "выберите официальный файл", map[string]string{"file": "обязательное поле"}, err))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, service.MaxContentBytes+1))
	if err != nil {
		httpx.WriteError(w, apperror.New(apperror.KindValidation, "invalid_normative_upload", "не удалось прочитать файл", nil, err))
		return
	}
	contentType := strings.TrimSpace(r.FormValue("content_type"))
	if contentType == "" {
		contentType = header.Header.Get("Content-Type")
	}
	source, diff, err := h.service.Import(r.Context(), domain.Import{ActCode: r.FormValue("act_code"), Title: r.FormValue("title"), Revision: r.FormValue("revision"), PublishedOn: r.FormValue("published_on"), EffectiveOn: r.FormValue("effective_on"), SourceURL: r.FormValue("source_url"), ExpectedSHA256: r.FormValue("sha256"), ContentType: contentType, OriginalFilename: header.Filename, ImportedBy: actorID, Content: content})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"source": source, "diff": diff})
}
