package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const egrulBaseURL = "https://egrul.nalog.ru"
const directoryEnrichmentLockID int64 = 1129989445

type egrulSearchTicket struct {
	Token           string `json:"t"`
	CaptchaRequired bool   `json:"captchaRequired"`
}

type egrulSearchRow struct {
	INN        string `json:"i"`
	OGRN       string `json:"o"`
	Name       string `json:"n"`
	Region     string `json:"rn"`
	ClosedDate string `json:"e"`
}

type egrulSearchResult struct {
	Rows []egrulSearchRow `json:"rows"`
}

type DirectoryEnrichmentResult struct {
	Processed int
	Matched   int
	Unmatched int
}

type egrulClient struct {
	baseURL string
	http    *http.Client
}

var organizationNameStopPrefixes = []string{
	"автономн", "академи", "бюджетн", "высш", "государственн", "институт",
	"негосударственн", "образован", "организац", "профессиональн", "университет",
	"учрежден", "федеральн", "филиал", "частн", "имени",
}

func organizationNameStopWord(word string) bool {
	for _, prefix := range organizationNameStopPrefixes {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return false
}

func organizationNameTokens(value string) map[string]bool {
	value = strings.ToLower(strings.ReplaceAll(value, "ё", "е"))
	words := strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	result := make(map[string]bool, len(words))
	for _, word := range words {
		if len([]rune(word)) < 3 || organizationNameStopWord(word) {
			continue
		}
		result[word] = true
	}
	return result
}

func tokenOverlap(left, right map[string]bool) int {
	count := 0
	for token := range left {
		if right[token] {
			count++
		}
	}
	return count
}

func organizationSearchQueries(name string) []string {
	normalizedQuotes := strings.NewReplacer("«", `"`, "»", `"`, "“", `"`, "”", `"`).Replace(name)
	parts := strings.Split(normalizedQuotes, `"`)
	preferred := ""
	for index := 1; index < len(parts); index += 2 {
		candidate := strings.TrimSpace(parts[index])
		if len(organizationNameTokens(candidate)) > len(organizationNameTokens(preferred)) {
			preferred = candidate
		}
	}
	if preferred != "" && !strings.EqualFold(preferred, strings.TrimSpace(name)) {
		return []string{preferred, name}
	}
	return []string{name}
}

func selectEGRULCandidate(rows []egrulSearchRow, name, region string) (egrulSearchRow, bool) {
	queryTokens := organizationNameTokens(name)
	regionTokens := organizationNameTokens(region)
	bestScore := 0.0
	var best egrulSearchRow
	for _, row := range rows {
		if !validINN(row.INN) || !validOGRN(row.OGRN) {
			continue
		}
		candidateTokens := organizationNameTokens(row.Name)
		if len(queryTokens) == 0 || len(candidateTokens) == 0 {
			continue
		}
		overlap := tokenOverlap(queryTokens, candidateTokens)
		minimumOverlap := 2
		if len(queryTokens) == 1 && len(candidateTokens) == 1 {
			minimumOverlap = 1
		}
		if overlap < minimumOverlap {
			continue
		}
		candidateCoverage := float64(overlap) / float64(len(candidateTokens))
		queryCoverage := float64(overlap) / float64(len(queryTokens))
		if candidateCoverage < 0.6 || queryCoverage < 0.45 {
			continue
		}
		score := 0.7*candidateCoverage + 0.3*queryCoverage
		candidateRegion := organizationNameTokens(row.Region)
		if len(regionTokens) > 0 && tokenOverlap(regionTokens, candidateRegion) > 0 {
			score += 0.1
		}
		if row.ClosedDate == "" {
			score += 0.05
		}
		if score > bestScore {
			bestScore = score
			best = row
		}
	}
	return best, bestScore >= 0.55
}

func (c egrulClient) search(ctx context.Context, name string) (*egrulSearchResult, error) {
	form := url.Values{"query": {name}, "region": {""}, "PreventChromeAutocomplete": {""}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "CyberCalculatorMINC directory review/1.0")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ФНС вернула HTTP %d", response.StatusCode)
	}
	var ticket egrulSearchTicket
	if err := json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		return nil, fmt.Errorf("некорректный ответ ФНС: %w", err)
	}
	if ticket.CaptchaRequired {
		return nil, fmt.Errorf("ФНС запросила CAPTCHA; повторите обогащение позднее")
	}
	if ticket.Token == "" {
		return nil, fmt.Errorf("ФНС не вернула идентификатор поиска")
	}

	resultURL := c.baseURL + "/search-result/" + url.PathEscape(ticket.Token)
	for attempt := 0; attempt < 10; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, resultURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "CyberCalculatorMINC directory review/1.0")
		response, err = c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("ФНС вернула HTTP %d", response.StatusCode)
		}
		var result egrulSearchResult
		decodeErr := json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("некорректный результат поиска ФНС: %w", decodeErr)
		}
		if len(result.Rows) > 0 {
			return &result, nil
		}
	}
	return &egrulSearchResult{}, nil
}

// EnrichEducationDirectory fills missing identifiers from a strongly matching
// result of the official EGRUL search. Automated matches are deliberately never
// trusted: each processed record remains pending until a reviewer confirms it.
func EnrichEducationDirectory(ctx context.Context, db *sql.DB, limit int, delay time.Duration) (DirectoryEnrichmentResult, error) {
	if limit < 0 {
		return DirectoryEnrichmentResult{}, fmt.Errorf("лимит не может быть отрицательным")
	}
	lockConn, err := db.Conn(ctx)
	if err != nil {
		return DirectoryEnrichmentResult{}, err
	}
	defer lockConn.Close()
	var locked bool
	if err := lockConn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, directoryEnrichmentLockID).Scan(&locked); err != nil {
		return DirectoryEnrichmentResult{}, err
	}
	if !locked {
		return DirectoryEnrichmentResult{}, nil
	}
	defer lockConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, directoryEnrichmentLockID)

	query := `SELECT id::text,name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
		COALESCE(license_number,''),license_status,institution_status,COALESCE(registry_record_id,''),
		COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),verification_status
		FROM education_directory WHERE verification_status IN ('monitoring_only','pending')
		AND (NULLIF(BTRIM(inn),'') IS NULL OR NULLIF(BTRIM(ogrn),'') IS NULL)
		ORDER BY updated_at,id`
	if limit > 0 {
		query += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return DirectoryEnrichmentResult{}, err
	}
	type candidate struct {
		values [13]string
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		dest := make([]interface{}, len(item.values))
		for index := range item.values {
			dest[index] = &item.values[index]
		}
		if err := rows.Scan(dest...); err != nil {
			rows.Close()
			return DirectoryEnrichmentResult{}, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return DirectoryEnrichmentResult{}, err
	}
	if err := rows.Err(); err != nil {
		return DirectoryEnrichmentResult{}, err
	}

	var runID string
	if err := db.QueryRowContext(ctx, `INSERT INTO directory_sync_runs(source_url,status) VALUES($1,'started') RETURNING id::text`, egrulBaseURL).Scan(&runID); err != nil {
		return DirectoryEnrichmentResult{}, err
	}
	result := DirectoryEnrichmentResult{}
	client := egrulClient{baseURL: egrulBaseURL, http: &http.Client{Timeout: 30 * time.Second}}
	fail := func(cause error) (DirectoryEnrichmentResult, error) {
		db.ExecContext(context.Background(), `UPDATE directory_sync_runs SET status='failed',imported_count=$1,rejected_count=$2,error_text=$3,finished_at=now() WHERE id::text=$4`,
			result.Matched, result.Unmatched, cause.Error(), runID)
		return result, cause
	}

	for index, item := range candidates {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		id, name := item.values[0], item.values[1]
		if _, err := db.ExecContext(ctx, `UPDATE education_directory SET verification_status='pending',verified_at=NULL,verified_by=NULL,updated_at=now() WHERE id::text=$1`, id); err != nil {
			return fail(err)
		}
		var matched egrulSearchRow
		matchOK := false
		matchedResultCount := 0
		for queryIndex, searchName := range organizationSearchQueries(name) {
			if queryIndex > 0 {
				select {
				case <-ctx.Done():
					return fail(ctx.Err())
				case <-time.After(delay):
				}
			}
			search, searchErr := client.search(ctx, searchName)
			if searchErr != nil {
				return fail(fmt.Errorf("%s: %w", name, searchErr))
			}
			matched, matchOK = selectEGRULCandidate(search.Rows, searchName, item.values[3])
			if matchOK {
				matchedResultCount = len(search.Rows)
				break
			}
		}
		result.Processed++
		if !matchOK {
			result.Unmatched++
			log.Printf("обогащение справочника: не найден корректный кандидат для %q", name)
		} else {
			institutionStatus := "active"
			if matched.ClosedDate != "" {
				institutionStatus = "liquidated"
			}
			tx, txErr := db.BeginTx(ctx, nil)
			if txErr != nil {
				return fail(txErr)
			}
			oldValue := directoryAuditValue(item.values[1], item.values[2], item.values[3], item.values[4], item.values[5],
				item.values[6], item.values[7], item.values[8], item.values[9], item.values[10], item.values[11], item.values[12])
			_, txErr = tx.ExecContext(ctx, `UPDATE education_directory SET inn=$1,ogrn=$2,institution_status=$3,
				source_url=$4,registry_updated_at=CURRENT_DATE,verification_status='pending',verified_at=NULL,
				verified_by=NULL,updated_at=now() WHERE id::text=$5`, matched.INN, matched.OGRN, institutionStatus,
				egrulBaseURL+"/index.html", id)
			newValue := directoryAuditValue(item.values[1], item.values[2], item.values[3], matched.INN, matched.OGRN,
				item.values[6], item.values[7], institutionStatus, item.values[9], egrulBaseURL+"/index.html",
				time.Now().UTC().Format("2006-01-02"), "pending")
			if txErr == nil {
				txErr = logAudit(ctx, tx, "education_directory", id, "directory_enrich", "",
					fmt.Sprintf("Автоподбор ФНС: выбран лучший из %d результатов (%s, %s)", matchedResultCount, matched.Name, matched.Region), oldValue, newValue)
			}
			if txErr == nil {
				txErr = tx.Commit()
			} else {
				tx.Rollback()
			}
			if txErr != nil {
				return fail(txErr)
			}
			result.Matched++
			log.Printf("обогащение справочника: %d/%d %q -> ИНН %s, ОГРН %s; требуется проверка", index+1, len(candidates), name, matched.INN, matched.OGRN)
		}
		if delay > 0 && index+1 < len(candidates) {
			select {
			case <-ctx.Done():
				return fail(ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE directory_sync_runs SET status='completed',imported_count=$1,rejected_count=$2,finished_at=now() WHERE id::text=$3`, result.Matched, result.Unmatched, runID); err != nil {
		return result, err
	}
	return result, nil
}
