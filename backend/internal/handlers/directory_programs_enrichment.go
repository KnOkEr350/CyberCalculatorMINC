package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

var monitoringIDPattern = regexp.MustCompile(`https://monitoring\.miccedu\.ru/iam/[0-9]{4}/_vpo/inst\.php\?id=[0-9]+`)

func programsHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
				return nil, fmt.Errorf("источник указывает на непубличный адрес")
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("нет адреса источника")
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !publicProgramURL(req.URL.String()) {
			return fmt.Errorf("недопустимый редирект")
		}
		return nil
	}}
}

func fetchProgramPage(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	if !publicProgramURL(rawURL) {
		return "", fmt.Errorf("нужен HTTPS-источник")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "CyberCalculator-directory/1.0")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if len(body) > 8<<20 {
		return "", fmt.Errorf("страница превышает 8 МБ")
	}
	return string(body), err
}

func universityProgramURL(body string) string {
	for _, row := range educationRowPattern.FindAllStringSubmatch(body, -1) {
		cells := educationCellPattern.FindAllStringSubmatch(row[1], -1)
		if len(cells) != 2 || !strings.Contains(cellText(cells[0][1]), "web-сайт") {
			continue
		}
		site, err := url.Parse(cellText(cells[1][1]))
		if err != nil || site.Hostname() == "" {
			return ""
		}
		site.Scheme, site.RawQuery, site.Fragment = "https", "", ""
		site.Path = strings.TrimRight(site.Path, "/") + "/sveden/education/"
		return site.String()
	}
	return ""
}

// Uses each monitoring record's own website, including branch websites.
// A network failure never clears known programs or confirms a licence.
func EnrichDirectoryPrograms(ctx context.Context, db *sql.DB, limit int) (DirectoryEnrichmentResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := DirectoryEnrichmentResult{}
	startedAt := time.Now()
	if limit < 0 {
		return result, fmt.Errorf("отрицательный лимит")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(1129989446)`).Scan(&locked); err != nil || !locked {
		return result, err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(1129989446)`)
	rows, err := db.QueryContext(ctx, `SELECT id::text,source,programs_source_url FROM education_directory
	 WHERE partner_kind='vuz' AND (programs_checked_at IS NULL OR programs_checked_at<now()-interval '35 days')
	 ORDER BY programs_attempted_at NULLS FIRST,id LIMIT NULLIF($1,0)`, limit)
	if err != nil {
		return result, err
	}
	type candidate struct{ id, source, programsURL string }
	items := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.id, &item.source, &item.programsURL); err != nil {
			rows.Close()
			return result, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	client := programsHTTPClient()
	defer client.CloseIdleConnections()
	jobs := make(chan candidate)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if _, attemptErr := db.ExecContext(ctx, `UPDATE education_directory SET programs_attempted_at=now() WHERE id::text=$1`, item.id); attemptErr != nil {
					mu.Lock()
					if firstError == nil {
						firstError = attemptErr
					}
					mu.Unlock()
					cancel()
					return
				}
				pageURL := item.programsURL
				if pageURL == "" {
					body, _ := fetchProgramPage(ctx, client, monitoringIDPattern.FindString(item.source))
					pageURL = universityProgramURL(body)
				}
				body, pageErr := fetchProgramPage(ctx, client, pageURL)
				codes := parseEducationPrograms(body)
				matched := pageErr == nil && len(codes) > 0
				if matched {
					tx, saveErr := db.BeginTx(ctx, nil)
					if saveErr == nil {
						var oldCodes pq.StringArray
						var oldURL string
						var checkedAt sql.NullTime
						saveErr = tx.QueryRowContext(ctx, `SELECT program_codes,programs_source_url,programs_checked_at FROM education_directory WHERE id::text=$1 FOR UPDATE`, item.id).Scan(&oldCodes, &oldURL, &checkedAt)
						if saveErr == nil && checkedAt.Valid && !checkedAt.Time.Before(startedAt) {
							tx.Rollback()
							continue
						}
						if saveErr == nil {
							_, saveErr = tx.ExecContext(ctx, `UPDATE education_directory SET program_codes=$2,programs_source_url=$3,programs_checked_at=now(),updated_at=now() WHERE id::text=$1`, item.id, pq.Array(codes), pageURL)
						}
						if saveErr == nil {
							saveErr = logAudit(tx, "education_directory", item.id, "directory_programs", "", "Программы с сайта учебного заведения", map[string]interface{}{"program_codes": oldCodes, "programs_source_url": oldURL}, map[string]interface{}{"program_codes": codes, "programs_source_url": pageURL})
						}
						if saveErr == nil {
							saveErr = tx.Commit()
						} else {
							tx.Rollback()
						}
					}
					if saveErr != nil {
						matched = false
						mu.Lock()
						if firstError == nil {
							firstError = saveErr
						}
						mu.Unlock()
					}
				}
				mu.Lock()
				result.Processed++
				if matched {
					result.Matched++
				} else {
					result.Unmatched++
				}
				if result.Processed%25 == 0 {
					log.Printf("направления: обработано %d, получены коды %d", result.Processed, result.Matched)
				}
				mu.Unlock()
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}()
	}
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		select {
		case jobs <- item:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, firstError
}
