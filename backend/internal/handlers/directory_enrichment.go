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
)

const egrulBaseURL = "https://egrul.nalog.ru"

type egrulSearchTicket struct {
	Token           string `json:"t"`
	CaptchaRequired bool   `json:"captchaRequired"`
}

type egrulSearchResult struct {
	Rows []struct {
		INN        string `json:"i"`
		OGRN       string `json:"o"`
		Name       string `json:"n"`
		Region     string `json:"rn"`
		ClosedDate string `json:"e"`
	} `json:"rows"`
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

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/search-result/"+url.PathEscape(ticket.Token), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "CyberCalculatorMINC directory review/1.0")
	response, err = c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ФНС вернула HTTP %d", response.StatusCode)
	}
	var result egrulSearchResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("некорректный результат поиска ФНС: %w", err)
	}
	return &result, nil
}

// EnrichEducationDirectory fills missing identifiers from the first result of
// the official EGRUL search. Automated matches are deliberately never trusted:
// each processed record remains pending until a signed-in reviewer confirms it.
func EnrichEducationDirectory(ctx context.Context, db *sql.DB, limit int, delay time.Duration) (DirectoryEnrichmentResult, error) {
	if limit < 0 {
		return DirectoryEnrichmentResult{}, fmt.Errorf("лимит не может быть отрицательным")
	}
	query := `SELECT id::text,name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
		COALESCE(license_number,''),license_status,institution_status,COALESCE(registry_record_id,''),
		COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),verification_status
		FROM education_directory WHERE verification_status IN ('monitoring_only','pending')
		AND (NULLIF(BTRIM(inn),'') IS NULL OR NULLIF(BTRIM(ogrn),'') IS NULL)
		ORDER BY name,id`
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
		search, err := client.search(ctx, name)
		result.Processed++
		if err != nil {
			return fail(fmt.Errorf("%s: %w", name, err))
		}
		if len(search.Rows) == 0 || !validINN(search.Rows[0].INN) || !validOGRN(search.Rows[0].OGRN) {
			result.Unmatched++
			log.Printf("обогащение справочника: не найден корректный кандидат для %q", name)
		} else {
			first := search.Rows[0]
			institutionStatus := "active"
			if first.ClosedDate != "" {
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
				verified_by=NULL,updated_at=now() WHERE id::text=$5`, first.INN, first.OGRN, institutionStatus,
				egrulBaseURL+"/index.html", id)
			newValue := directoryAuditValue(item.values[1], item.values[2], item.values[3], first.INN, first.OGRN,
				item.values[6], item.values[7], institutionStatus, item.values[9], egrulBaseURL+"/index.html",
				time.Now().UTC().Format("2006-01-02"), "pending")
			if txErr == nil {
				txErr = logAudit(tx, "education_directory", id, "directory_enrich", "",
					fmt.Sprintf("Автоподбор ФНС: выбран первый из %d результатов (%s, %s)", len(search.Rows), first.Name, first.Region), oldValue, newValue)
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
			log.Printf("обогащение справочника: %d/%d %q -> ИНН %s, ОГРН %s; требуется проверка", index+1, len(candidates), name, first.INN, first.OGRN)
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
