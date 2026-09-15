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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
)

var monitoringIDPattern = regexp.MustCompile(`https://monitoring\.miccedu\.ru/iam/[0-9]{4}/_vpo/inst\.php\?id=[0-9]+`)
var educationLinkPattern = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)

func educationProgramLinks(body, pageURL string) []string {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil
	}
	links := []string{}
	seen := map[string]bool{}
	for _, match := range educationLinkPattern.FindAllStringSubmatch(body, -1) {
		label := strings.ToLower(cellText(match[2]))
		if !strings.Contains(label, "образовательн") || !strings.Contains(label, "программ") {
			continue
		}
		excluded := false
		for _, word := range []string{"прием", "приём", "перевод", "отчислен", "восстановлен", "трудоустр", "адаптирован", "научн"} {
			if strings.Contains(label, word) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		ref, err := url.Parse(match[1])
		if err != nil {
			continue
		}
		target := base.ResolveReference(ref)
		target.Fragment = ""
		value := target.String()
		if target.Hostname() == base.Hostname() && strings.Contains(target.Path, "/sveden/education/") && publicProgramURL(value) && value != pageURL && !seen[value] {
			links = append(links, value)
			seen[value] = true
			if len(links) == 3 {
				break
			}
		}
	}
	return links
}

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
		// Prefer IPv4 on hosts that publish IPv6 even when the deployment has
		// no IPv6 route, and try remaining public addresses after a failure.
		sort.SliceStable(ips, func(i, j int) bool { return ips[i].IP.To4() != nil && ips[j].IP.To4() == nil })
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}}
	return &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Older university sites still advertise an HTTP redirect to their new
		// domain. Try that destination over HTTPS without sending an HTTP request.
		if req.URL.Scheme == "http" && (req.URL.Port() == "" || req.URL.Port() == "80") {
			req.URL.Scheme = "https"
			req.URL.Host = req.URL.Hostname()
		}
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
	body, err := io.ReadAll(io.LimitReader(res.Body, (24<<20)+1))
	if len(body) > 24<<20 {
		return "", fmt.Errorf("страница превышает 24 МБ")
	}
	return string(body), err
}

func universityProgramURL(body string) string {
	for _, row := range educationRowPattern.FindAllStringSubmatch(body, -1) {
		cells := educationCellPattern.FindAllStringSubmatch(row[1], -1)
		if len(cells) != 2 || !strings.Contains(cellText(cells[0][1]), "web-сайт") {
			continue
		}
		raw := cellText(cells[1][1])
		if raw != "" && !strings.Contains(raw, "://") {
			raw = "https://" + strings.TrimPrefix(raw, "//")
		}
		site, err := url.Parse(raw)
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
	return EnrichDirectoryProgramsForQuery(ctx, db, limit, "")
}

func EnrichDirectoryProgramsForQuery(ctx context.Context, db *sql.DB, limit int, query string) (DirectoryEnrichmentResult, error) {
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
	 AND ($2='' OR name ILIKE '%'||$2||'%')
	 ORDER BY (name ILIKE '%филиал%'),programs_attempted_at NULLS FIRST,id LIMIT NULLIF($1,0)`, limit, query)
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
					body, sourceErr := fetchProgramPage(ctx, client, monitoringIDPattern.FindString(item.source))
					if sourceErr != nil {
						log.Printf("направления %s: источник мониторинга: %v", item.id, sourceErr)
					}
					pageURL = universityProgramURL(body)
				}
				body, pageErr := fetchProgramPage(ctx, client, pageURL)
				if pageErr != nil {
					// Monitoring records sometimes retain a www alias which no
					// longer has a valid certificate. Use the same site's bare host.
					if target, err := url.Parse(pageURL); err == nil && strings.HasPrefix(target.Hostname(), "www.") {
						target.Host = strings.TrimPrefix(target.Host, "www.")
						fallbackBody, fallbackErr := fetchProgramPage(ctx, client, target.String())
						if fallbackErr == nil {
							body, pageErr, pageURL = fallbackBody, nil, target.String()
						}
					}
				}
				if pageErr != nil {
					log.Printf("направления %s: %s: %v", item.id, pageURL, pageErr)
				}
				codes := parseEducationPrograms(body)
				if len(codes) == 0 && pageErr == nil {
					for _, linkedURL := range educationProgramLinks(body, pageURL) {
						linkedBody, linkedErr := fetchProgramPage(ctx, client, linkedURL)
						if linkedErr == nil {
							codes = parseEducationPrograms(linkedBody)
							if len(codes) > 0 {
								pageURL = linkedURL
								break
							}
						}
					}
				}
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

// Keep missing program data moving through the queue without a manual CLI run.
func RunDirectoryPrograms(db *sql.DB, limit int, interval time.Duration, stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		result, err := EnrichDirectoryPrograms(ctx, db, limit)
		if ctx.Err() != nil {
			return
		}
		log.Printf("обновление направлений: обработано %d, получены коды %d, ошибка %v", result.Processed, result.Matched, err)
		select {
		case <-stop:
			return
		case <-time.After(interval):
		}
	}
}
