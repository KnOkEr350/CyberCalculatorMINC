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

var directoryColumns = []struct {
	Key   string
	Label string
}{
	{"name", "Наименование"},
	{"partner_kind", "Тип учебного заведения"},
	{"region", "Регион"},
	{"inn", "ИНН"},
	{"ogrn", "ОГРН / ОГРНИП"},
	{"license_number", "Номер лицензии"},
	{"license_status", "Статус лицензии"},
	{"institution_status", "Статус организации"},
	{"registry_record_id", "Идентификатор записи реестра"},
	{"source_url", "Ссылка на официальный источник"},
	{"registry_updated_at", "Дата актуальности сведений"},
}

func normalizeDirectoryValue(column, value string) string {
	value = strings.TrimSpace(value)
	mappings := map[string]map[string]string{
		"partner_kind":       {"Вуз": "vuz", "Колледж": "kolledj", "Школа": "school"},
		"license_status":     {"Действует": "active", "Приостановлена": "suspended", "Истекла": "expired", "Аннулирована": "revoked", "Не указан": "unknown"},
		"institution_status": {"Действует": "active", "Не действует": "inactive", "Реорганизована": "reorganized", "Ликвидирована": "liquidated", "Не указан": "unknown"},
	}
	for label, normalized := range mappings[column] {
		if strings.EqualFold(value, label) {
			return normalized
		}
	}
	return value
}

func freshRegistryDate(date, now time.Time) bool {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !date.After(today) && !date.Before(today.AddDate(0, 0, -35))
}

func validateDirectoryRows(rows [][]string) ([][]string, []string) {
	if len(rows) == 0 || (len(rows[0]) != len(directoryColumns) && len(rows[0]) != len(directoryColumns)+1) {
		return nil, []string{"используйте заголовки из шаблона справочника"}
	}
	hasReviewAction := len(rows[0]) == len(directoryColumns)+1
	for index, column := range directoryColumns {
		header := strings.TrimSpace(rows[0][index])
		if header != column.Label && header != column.Key {
			return nil, []string{"используйте заголовки из шаблона справочника"}
		}
	}
	if hasReviewAction {
		header := strings.TrimSpace(rows[0][len(directoryColumns)])
		if header != "Действие" && header != "review_action" {
			return nil, []string{"используйте заголовки из шаблона справочника"}
		}
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
		expectedColumns := len(directoryColumns)
		if hasReviewAction {
			expectedColumns++
		}
		if len(row) > expectedColumns {
			errors = append(errors, fmt.Sprintf("Строка %d: есть данные за пределами заголовков", i+2))
			continue
		}
		for len(row) < expectedColumns {
			row = append(row, "")
		}
		for j := range directoryColumns {
			row[j] = normalizeDirectoryValue(directoryColumns[j].Key, row[j])
		}
		if hasReviewAction {
			action := strings.TrimSpace(row[len(directoryColumns)])
			if action == "" || strings.EqualFold(action, "Оставить без подтверждения") || strings.EqualFold(action, "skip") {
				continue
			}
			if !strings.EqualFold(action, "Подтвердить") && !strings.EqualFold(action, "confirm") {
				errors = append(errors, fmt.Sprintf("Строка %d: в столбце «Действие» укажите «Подтвердить» или оставьте пустым", i+2))
				continue
			}
			row = row[:len(directoryColumns)]
		}
		date, dateErr := time.Parse("2006-01-02", row[10])
		licenseOK := row[6] == "active" || row[6] == "suspended" || row[6] == "expired" || row[6] == "revoked" || row[6] == "unknown"
		institutionOK := row[7] == "active" || row[7] == "inactive" || row[7] == "reorganized" || row[7] == "liquidated" || row[7] == "unknown"
		if len(row) != 11 || len([]rune(row[0])) < 2 || len([]rune(row[0])) > 1000 ||
			(row[1] != "vuz" && row[1] != "kolledj" && row[1] != "school") || len([]rune(row[2])) < 2 || len([]rune(row[2])) > 200 ||
			!validINN(row[3]) || !validOGRN(row[4]) || len([]rune(row[5])) > 100 || !licenseOK || !institutionOK ||
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

func upsertDirectoryRows(ctx context.Context, tx *sql.Tx, rows [][]string, verificationStatus, userID string, audit bool) error {
	for _, row := range rows {
		var id string
		var oldName, oldKind, oldRegion, oldINN, oldOGRN, oldLicenseNumber, oldLicenseStatus, oldInstitutionStatus string
		var oldRecordID, oldSourceURL, oldUpdatedAt, oldVerificationStatus string
		oldErr := tx.QueryRowContext(ctx, `SELECT id::text,name,partner_kind,region,COALESCE(inn,''),COALESCE(ogrn,''),
			COALESCE(license_number,''),license_status,institution_status,COALESCE(registry_record_id,''),
			COALESCE(source_url,''),COALESCE(registry_updated_at::text,''),verification_status
			FROM education_directory WHERE registry_record_id=$1 OR (name=$2 AND partner_kind=$3 AND region=$4)
			ORDER BY (registry_record_id=$1) DESC LIMIT 1 FOR UPDATE`, row[8], row[0], row[1], row[2]).
			Scan(&id, &oldName, &oldKind, &oldRegion, &oldINN, &oldOGRN, &oldLicenseNumber,
				&oldLicenseStatus, &oldInstitutionStatus, &oldRecordID, &oldSourceURL, &oldUpdatedAt, &oldVerificationStatus)
		if oldErr != nil && oldErr != sql.ErrNoRows {
			return oldErr
		}
		result, err := tx.ExecContext(ctx, `UPDATE education_directory SET name=$1,partner_kind=$2,region=$3,source=$10,
			inn=$4,ogrn=$5,license_number=$6,license_status=$7,institution_status=$8,source_url=$10,
			registry_updated_at=$11::date,verified_at=CASE WHEN $12='verified' THEN now() ELSE NULL END,
			verified_by=NULLIF($13,'')::uuid,verification_status=$12,updated_at=now()
			WHERE registry_record_id=$9`, row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8], row[9], row[10], verificationStatus, userID)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated == 0 {
			err = tx.QueryRowContext(ctx, `INSERT INTO education_directory(
				name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,
				registry_record_id,source_url,registry_updated_at,verified_at,verified_by,verification_status)
				VALUES($1,$2,$3,$10,$4,$5,$6,$7,$8,$9,$10,$11::date,
				CASE WHEN $12='verified' THEN now() ELSE NULL END,NULLIF($13,'')::uuid,$12)
				ON CONFLICT(name,partner_kind,region) DO UPDATE SET source=EXCLUDED.source,inn=EXCLUDED.inn,ogrn=EXCLUDED.ogrn,
				license_number=EXCLUDED.license_number,license_status=EXCLUDED.license_status,institution_status=EXCLUDED.institution_status,
				registry_record_id=EXCLUDED.registry_record_id,source_url=EXCLUDED.source_url,
				registry_updated_at=EXCLUDED.registry_updated_at,verified_at=EXCLUDED.verified_at,
				verified_by=EXCLUDED.verified_by,verification_status=EXCLUDED.verification_status,updated_at=now()
				RETURNING id::text`,
				row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8], row[9], row[10], verificationStatus, userID).Scan(&id)
		}
		if err != nil {
			return err
		}
		if id == "" {
			if err := tx.QueryRowContext(ctx, `SELECT id::text FROM education_directory WHERE registry_record_id=$1`, row[8]).Scan(&id); err != nil {
				return err
			}
		}
		if audit {
			var oldValue interface{}
			if oldErr == nil {
				oldValue = directoryAuditValue(oldName, oldKind, oldRegion, oldINN, oldOGRN,
					oldLicenseNumber, oldLicenseStatus, oldInstitutionStatus, oldRecordID,
					oldSourceURL, oldUpdatedAt, oldVerificationStatus)
			}
			newValue := directoryAuditValue(row[0], row[1], row[2], row[3], row[4], row[5],
				row[6], row[7], row[8], row[9], row[10], verificationStatus)
			if err := logAudit(tx, "education_directory", id, "directory_confirm", userID,
				"Подтверждено через Excel", oldValue, newValue); err != nil {
				return err
			}
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
	if err = upsertDirectoryRows(ctx, tx, valid, "pending", "", false); err != nil {
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
