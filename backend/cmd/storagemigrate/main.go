// Command storagemigrate переносит исторические вложения в адресуемое по
// содержимому хранилище (STORE-06). Запускается вручную при обновлении и
// безопасен для повторного запуска.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"cybercalc/internal/storagemigrate"

	_ "github.com/lib/pq"
)

const sizeLimit int64 = 20 << 20

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: storagemigrate <database-dsn> <upload-dir>")
		os.Exit(2)
	}
	db, err := sql.Open("postgres", os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "storage migrate:", err)
		os.Exit(1)
	}
	defer db.Close()
	report, err := storagemigrate.Run(context.Background(), db, os.Args[2], sizeLimit)
	fmt.Println("storage migrate:", report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "storage migrate:", err)
		os.Exit(1)
	}
	// Расхождения и пропажи требуют ручного разбора: они не ошибка прогона,
	// но и молча закрывать перенос на них нельзя.
	if report.Missing > 0 || report.Mismatched > 0 {
		fmt.Fprintln(os.Stderr, "storage migrate: остались строки для ручного разбора")
		os.Exit(3)
	}
}
