package handlers

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var programCodePattern = regexp.MustCompile(`^[0-9]{2}\.(01|02|03|04|05|06)\.[0-9]{2}$`)
var programCodeInCell = regexp.MustCompile(`\b[0-9]{2}\.(?:01|02|03|04|05|06)\.[0-9]{2}\b`)
var educationRowPattern = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
var educationCellPattern = regexp.MustCompile(`(?is)<(?:td|th)\b[^>]*>(.*?)</(?:td|th)>`)
var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)
var nextDataPattern = regexp.MustCompile(`(?is)<script\b[^>]*\bid=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)
var scriptPattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
var programElementPattern = regexp.MustCompile(`(?is)<(?:td|p|span|div)\b[^>]*\bitemprop=["']eduCode["'][^>]*>(.*?)</(?:td|p|span|div)>`)

func normalizeProgramCodes(codes []string) ([]string, error) {
	seen := map[string]bool{}
	result := []string{}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if !programCodePattern.MatchString(code) || strings.HasSuffix(code, ".00") {
			return nil, fmt.Errorf("укажите точный код направления, например 09.03.01; укрупнённой группы недостаточно")
		}
		if !seen[code] {
			seen[code] = true
			result = append(result, code)
		}
	}
	if len(result) > 500 {
		return nil, fmt.Errorf("не более 500 кодов направлений")
	}
	sort.Strings(result)
	return result, nil
}

func publicProgramURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && (u.Port() == "" || u.Port() == "443")
}

func cellText(raw string) string {
	return strings.Join(strings.Fields(html.UnescapeString(htmlTagPattern.ReplaceAllString(raw, " "))), " ")
}

// Only program rows count, not navigation, scripts or broad xx.00.00 groups.
func parseEducationPrograms(body string) []string {
	codes := []string{}
	// Some universities deliver their education tables as escaped HTML inside
	// Next.js page data. Read only that structured page payload, never arbitrary
	// script text, which can contain unrelated example codes.
	for _, match := range nextDataPattern.FindAllStringSubmatch(body, -1) {
		var data interface{}
		if json.Unmarshal([]byte(match[1]), &data) == nil {
			var visit func(interface{})
			visit = func(value interface{}) {
				switch v := value.(type) {
				case map[string]interface{}:
					for _, child := range v {
						visit(child)
					}
				case []interface{}:
					for _, child := range v {
						visit(child)
					}
				case string:
					if strings.Contains(v, "eduCode") {
						codes = append(codes, parseEducationPrograms(v)...)
					}
				}
			}
			visit(data)
		}
	}
	body = scriptPattern.ReplaceAllString(body, "")
	for _, element := range programElementPattern.FindAllStringSubmatch(body, -1) {
		codes = append(codes, programCodeInCell.FindAllString(cellText(element[1]), -1)...)
	}
	for _, row := range educationRowPattern.FindAllStringSubmatch(body, -1) {
		for _, cell := range educationCellPattern.FindAllStringSubmatch(row[1], -1) {
			value := cellText(cell[1])
			if programCodePattern.MatchString(value) || strings.Contains(cell[0], "eduCode") {
				codes = append(codes, programCodeInCell.FindAllString(value, -1)...)
			}
		}
	}
	result, _ := normalizeProgramCodes(codes)
	return result
}
