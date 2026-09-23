// Command backfill дозаполняет старые записи после смены модели данных.
// Запускается вручную при обновлении и безопасен для повторного запуска.
//
//	backfill teachers <dsn>
//	backfill directories <dsn>
//	backfill reconcile <dsn>
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"cybercalc/internal/backfill"

	_ "github.com/lib/pq"
)

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "teachers" && os.Args[1] != "directories" && os.Args[1] != "reconcile") {
		fmt.Fprintln(os.Stderr, "usage: backfill teachers|directories|reconcile <database-dsn>")
		os.Exit(2)
	}
	db, err := sql.Open("postgres", os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "backfill:", err)
		os.Exit(1)
	}
	defer db.Close()
	if os.Args[1] == "reconcile" {
		report, err := backfill.Reconcile(context.Background(), db)
		fmt.Println("backfill reconcile:", report)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backfill:", err)
			os.Exit(1)
		}
		// 3 — сверка не пройдена или остались находки: переходить на новую
		// модель и отключать старое чтение рано.
		if !report.ReadyForCutover() {
			os.Exit(3)
		}
		return
	}
	if os.Args[1] == "directories" {
		report, err := backfill.Directories(context.Background(), db)
		fmt.Println("backfill directories:", report)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backfill:", err)
			os.Exit(1)
		}
		if report.Total > 0 {
			fmt.Fprintln(os.Stderr, "backfill: остались строки справочников для уточнения (GET /api/admin/legacy-directory-findings)")
			os.Exit(3)
		}
		return
	}
	report, err := backfill.Teachers(context.Background(), db)
	fmt.Println("backfill teachers:", report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "backfill:", err)
		os.Exit(1)
	}
	// Записи на ручное уточнение — не сбой прогона, но и закрывать перенос на
	// них нельзя: код возврата 3 напоминает оператору разобрать список.
	if report.Manual() > 0 {
		fmt.Fprintln(os.Stderr, "backfill: остались записи для ручного уточнения (GET /api/admin/legacy-findings)")
		os.Exit(3)
	}
}
