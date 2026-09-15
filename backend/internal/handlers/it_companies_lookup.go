package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

const itLookupURL = "https://www.proreestr.ru/it-akkreditaciya/"

var itArticlePattern = regexp.MustCompile(`(?is)<article\b[^>]*>(.*?)</article>`)
var itHeadingPattern = regexp.MustCompile(`(?is)<h3\b[^>]*>(.*?)</h3>`)
var itINNPattern = regexp.MustCompile(`ИНН\s+([0-9]{10})\b`)

func parseITLookup(body, query string, now time.Time) ([]models.ITCompany, error) {
	items := []models.ITCompany{}
	seen := map[string]bool{}
	for _, article := range itArticlePattern.FindAllStringSubmatch(body, -1) {
		text := cellText(article[1])
		heading := itHeadingPattern.FindStringSubmatch(article[1])
		inn := itINNPattern.FindStringSubmatch(text)
		if len(heading) != 2 || len(inn) != 2 || !validINN(inn[1]) || !strings.Contains(text, "Аккредитована") || seen[inn[1]] {
			continue
		}
		seen[inn[1]] = true
		items = append(items, models.ITCompany{
			Name: cellText(heading[1]), INN: inn[1], AccreditationStatus: "active",
			RegistryRecordID: inn[1], RegistryUpdatedAt: now.UTC().Format("2006-01-02"),
			SourceURL: itLookupURL + "?" + url.Values{"q": {query}}.Encode(),
			Notes:     "Проверка реестра Госуслуг через публичный сервис ПроРеестр. ОГРН и номер аккредитации в ответе не предоставлены.",
		})
	}
	if len(items) == 0 && !strings.Contains(cellText(body), "аккредитация не найдена") {
		return nil, fmt.Errorf("сервис проверки реестра временно недоступен; повторите поиск или проверьте компанию на Госуслугах")
	}
	return items, nil
}

// The public registry is a search service. Keep it distinct from the local
// imported records and show its intermediary and missing fields explicitly.
func (h *ITCompanyHandlers) RegistrySearch(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
	if !canManageITCompanies(u) {
		middleware.WriteError(w, 403, "реестр ИТ-компаний недоступен для этого профиля")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	initial := query == ""
	if initial {
		query = "общество"
	}
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 200 {
		middleware.WriteError(w, 400, "введите от 2 до 200 символов названия или ИНН")
		return
	}
	client := programsHTTPClient()
	defer client.CloseIdleConnections()
	body, err := fetchProgramPage(r.Context(), client, itLookupURL+"?"+url.Values{"q": {query}}.Encode())
	if err != nil {
		middleware.WriteError(w, 502, "сервис проверки реестра временно недоступен; повторите поиск или проверьте компанию на Госуслугах")
		return
	}
	items, err := parseITLookup(body, query, time.Now())
	if err != nil {
		middleware.WriteError(w, 502, err.Error())
		return
	}
	middleware.WriteJSON(w, 200, map[string]interface{}{"items": items, "initial": initial, "may_have_more": len(itArticlePattern.FindAllStringSubmatch(body, -1)) >= 100})
}
