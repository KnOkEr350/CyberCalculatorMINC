// Package repository provides infrastructure adapters for the health module.
package repository

import (
	"context"
	"database/sql"
	"errors"
)

var ErrDatabaseNotConfigured = errors.New("database is not configured")

// Probe is the service-facing repository contract.
type Probe interface {
	Ping(context.Context) error
}

// SQLProbe checks the application's existing SQL connection pool.
type SQLProbe struct {
	db *sql.DB
}

func NewSQLProbe(db *sql.DB) *SQLProbe {
	return &SQLProbe{db: db}
}

func (p *SQLProbe) Ping(ctx context.Context) error {
	if p.db == nil {
		return ErrDatabaseNotConfigured
	}
	return p.db.PingContext(ctx)
}
