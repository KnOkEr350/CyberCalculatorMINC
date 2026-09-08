// Package dbx отвечает за подключение к PostgreSQL и применение миграций.
// database/sql — стандартная библиотека; github.com/lib/pq — единственный
// сторонний пакет во всём проекте (см. go.mod), он лишь реализует протокол
// PostgreSQL для database/sql и не является ORM/фреймворком.
package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/lib/pq"
)

func Connect(dsn string) (*sql.DB, error) {
	var db *sql.DB
	var err error
	for i := 0; i < 20; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				db.SetMaxOpenConns(20)
				db.SetMaxIdleConns(5)
				db.SetConnMaxLifetime(30 * time.Minute)
				return db, nil
			}
		}
		log.Printf("ожидание базы данных (попытка %d/20)...", i+1)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("не удалось подключиться к БД: %w", err)
}

// RunMigrations применяет .sql файлы из каталога по алфавиту, отслеживая
// уже применённые в таблице schema_migrations. Простая замена внешним
// инструментам вроде golang-migrate, чтобы не тянуть зависимость.
func RunMigrations(db *sql.DB, dir string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Несколько реплик backend могут запуститься одновременно. Advisory lock
	// гарантирует, что миграции в каждый момент применяет только одна из них.
	const migrationsLockID int64 = 1129989443
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationsLockID); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrationsLockID)

	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, f := range files {
		var already int
		conn.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename = $1`, f).Scan(&already)
		if already > 0 {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return err
		}
		log.Printf("применяю миграцию %s", f)
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("миграция %s: %w", f, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (filename) VALUES ($1)`, f); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
