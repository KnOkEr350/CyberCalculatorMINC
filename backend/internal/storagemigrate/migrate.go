// Package storagemigrate переносит исторические вложения в адресуемое по
// содержимому хранилище (STORE-06). Перенос идемпотентен: уже перенесённые
// файлы пропускаются, а расхождения не исправляются молча, а попадают в отчёт.
package storagemigrate

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"cybercalc/internal/filestore"
)

// Report — итог прогона. Каждая строка исходной таблицы попадает ровно в один
// счётчик, поэтому отчёт сходится с числом вложений.
type Report struct {
	AlreadyAddressed int // уже в CAS
	Migrated         int // перенесён, новый blob
	Deduplicated     int // перенесён, байты уже были в хранилище
	Missing          int // файла нет на диске
	Mismatched       int // записанный SHA-256 не совпал с содержимым
}

func (r Report) Total() int {
	return r.AlreadyAddressed + r.Migrated + r.Deduplicated + r.Missing + r.Mismatched
}

func (r Report) String() string {
	return fmt.Sprintf("всего %d: уже в CAS %d, перенесено %d, дедуплицировано %d, файл отсутствует %d, хеш не совпал %d",
		r.Total(), r.AlreadyAddressed, r.Migrated, r.Deduplicated, r.Missing, r.Mismatched)
}

type record struct {
	id, path string
	sha      sql.NullString
}

// Run переносит вложения пакетами. Предел размера совпадает с лимитом
// загрузки, иначе исторический файл не прошёл бы и при обычной отправке.
func Run(ctx context.Context, db *sql.DB, root string, sizeLimit int64) (Report, error) {
	var report Report
	rows, err := db.QueryContext(ctx, `SELECT id::text,storage_path,content_sha256 FROM attachments ORDER BY uploaded_at,id`)
	if err != nil {
		return report, fmt.Errorf("прочитать вложения: %w", err)
	}
	records := []record{}
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.path, &item.sha); err != nil {
			rows.Close()
			return report, err
		}
		records = append(records, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	for _, item := range records {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		if filestore.IsBlobPath(root, item.path) {
			report.AlreadyAddressed++
			continue
		}
		file, err := filestore.Open(root, item.path)
		if err != nil {
			// Файла нет — строку не трогаем: метаданные и аудит важнее, чем
			// аккуратность отчёта, и пропажу должен разобрать человек.
			report.Missing++
			continue
		}
		blob, err := filestore.CreateBlob(root, file, sizeLimit)
		file.Close()
		if err != nil {
			report.Missing++
			continue
		}
		// Сверка байтов: если в строке уже был хеш, содержимое обязано ему
		// соответствовать, иначе файл подменили и переносить его нельзя.
		if item.sha.Valid && item.sha.String != "" && item.sha.String != blob.SHA256 {
			report.Mismatched++
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE attachments SET storage_path=$1,content_sha256=$2 WHERE id::text=$3`,
			blob.Path, blob.SHA256, item.id); err != nil {
			return report, fmt.Errorf("обновить вложение %s: %w", item.id, err)
		}
		if blob.Deduplicated {
			report.Deduplicated++
		} else {
			report.Migrated++
		}
		// Старый файл убираем, только если на него не осталось ссылок.
		var referenced bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM attachments WHERE storage_path=$1)`, item.path).Scan(&referenced); err != nil {
			return report, err
		}
		if !referenced {
			if err := filestore.Remove(root, item.path); err != nil && !os.IsNotExist(err) {
				return report, fmt.Errorf("удалить исторический файл %s: %w", item.path, err)
			}
		}
	}
	return report, nil
}
