package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"cybercalc/internal/xlsx"
)

const directoryHeader = "name|partner_kind|region|inn|ogrn|license_number|license_status|institution_status|registry_record_id|source_url|registry_updated_at"

func freshRegistryDate(date, now time.Time) bool {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !date.After(today) && !date.Before(today.AddDate(0, 0, -35))
}

func validateDirectoryRows(rows [][]string) ([][]string, []string) {
	if len(rows) == 0 || strings.Join(rows[0], "|") != directoryHeader {
		return nil, []string{"используйте заголовки из шаблона справочника"}
	}
	if len(rows) > 10001 {
		return nil, []string{"не более 10000 организаций за один импорт"}
	}
	valid := make([][]string, 0, len(rows)-1)
	errors := []string{}
	seen := map[string]bool{}
	for i, row := range rows[1:] {
		if strings.Join(row, "") == "" {
			continue
		}
		for len(row) < 11 {
			row = append(row, "")
		}
		for j := range row {
			row[j] = strings.TrimSpace(row[j])
		}
		date, dateErr := time.Parse("2006-01-02", row[10])
		licenseOK := row[6] == "active" || row[6] == "suspended" || row[6] == "expired" || row[6] == "revoked"
		institutionOK := row[7] == "active" || row[7] == "inactive" || row[7] == "reorganized" || row[7] == "liquidated"
		if len(row) != 11 || len([]rune(row[0])) < 2 || len([]rune(row[0])) > 1000 ||
			(row[1] != "vuz" && row[1] != "kolledj" && row[1] != "school") || len([]rune(row[2])) < 2 || len([]rune(row[2])) > 200 ||
			!validINN(row[3]) || !validOGRN(row[4]) || row[5] == "" || len([]rune(row[5])) > 100 || !licenseOK || !institutionOK ||
			row[8] == "" || len([]rune(row[8])) > 200 || !officialRegistryURL(row[9]) || dateErr != nil || !freshRegistryDate(date, time.Now()) {
			errors = append(errors, fmt.Sprintf("Строка %d: проверьте название, тип, регион, ИНН/ОГРН, лицензию, статус, идентификатор, официальную ссылку и дату", i+2))
			continue
		}
		if seen[row[8]] {
			errors = append(errors, fmt.Sprintf("Строка %d: повторный идентификатор реестра", i+2))
			continue
		}
		seen[row[8]] = true
		valid = append(valid, row)
	}
	if len(valid) == 0 && len(errors) == 0 {
		errors = append(errors, "нет строк данных")
	}
	return valid, errors
}

func upsertVerifiedDirectoryRows(ctx context.Context, tx *sql.Tx, rows [][]string) error {
	for _, row := range rows {
		result, err := tx.ExecContext(ctx, `UPDATE education_directory SET name=$1,partner_kind=$2,region=$3,source=$10,
			inn=$4,ogrn=$5,license_number=$6,license_status=$7,institution_status=$8,source_url=$10,
			registry_updated_at=$11::date,verified_at=now(),verification_status='verified',updated_at=now()
			WHERE registry_record_id=$9`, row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8], row[9], row[10])
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO education_directory(
				name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,
				registry_record_id,source_url,registry_updated_at,verified_at,verification_status)
				VALUES($1,$2,$3,$10,$4,$5,$6,$7,$8,$9,$10,$11::date,now(),'verified')
				ON CONFLICT(name,partner_kind,region) DO UPDATE SET source=EXCLUDED.source,inn=EXCLUDED.inn,ogrn=EXCLUDED.ogrn,
				license_number=EXCLUDED.license_number,license_status=EXCLUDED.license_status,institution_status=EXCLUDED.institution_status,
				registry_record_id=EXCLUDED.registry_record_id,source_url=EXCLUDED.source_url,
				registry_updated_at=EXCLUDED.registry_updated_at,verified_at=now(),verification_status='verified',updated_at=now()`,
				row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8], row[9], row[10])
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RunDirectorySync downloads a normalized official XLSX snapshot immediately
// and then on the configured interval. An invalid row rejects the whole
// snapshot, so a partial/corrupt registry never replaces verified data.
func RunDirectorySync(db *sql.DB, sourceURL string, interval time.Duration, stop <-chan struct{}) {
	if sourceURL == "" {
		return
	}
	run := func() {
		if err := syncDirectoryOnce(db, sourceURL); err != nil {
			log.Printf("автообновление справочника: %v", err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			run()
		case <-stop:
			return
		}
	}
}

func syncDirectoryOnce(db *sql.DB, sourceURL string) error {
	if !officialRegistryURL(sourceURL) {
		return fmt.Errorf("неофициальный URL источника")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(1129989444)`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil // another backend replica is already refreshing the registry
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(1129989444)`)
	var runID string
	if err = conn.QueryRowContext(ctx, `INSERT INTO directory_sync_runs(source_url,status) VALUES($1,'started') RETURNING id`, sourceURL).Scan(&runID); err != nil {
		return err
	}
	fail := func(err error) error {
		message := err.Error()
		if len(message) > 2000 {
			message = message[:2000]
		}
		conn.ExecContext(context.Background(), `UPDATE directory_sync_runs SET status='failed',error_text=$1,finished_at=now() WHERE id=$2`, message, runID)
		return err
	}
	client := &http.Client{
		Timeout: 90 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || !officialRegistryURL(req.URL.String()) {
				return fmt.Errorf("переадресация за пределы официального источника запрещена")
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fail(err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fail(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("источник вернул HTTP %d", response.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		if err == nil {
			err = fmt.Errorf("выгрузка превышает 8 МБ")
		}
		return fail(err)
	}
	rows, err := xlsx.ReadFirst(data)
	if err != nil {
		return fail(fmt.Errorf("некорректный XLSX: %w", err))
	}
	valid, validationErrors := validateDirectoryRows(rows)
	if len(validationErrors) > 0 {
		return fail(fmt.Errorf("выгрузка отклонена (%d ошибок): %s", len(validationErrors), strings.Join(validationErrors[:min(5, len(validationErrors))], "; ")))
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	if err = upsertVerifiedDirectoryRows(ctx, tx, valid); err != nil {
		tx.Rollback()
		return fail(err)
	}
	if _, err = tx.Exec(`UPDATE directory_sync_runs SET status='completed',imported_count=$1,finished_at=now() WHERE id=$2`, len(valid), runID); err != nil {
		tx.Rollback()
		return fail(err)
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return nil
}
