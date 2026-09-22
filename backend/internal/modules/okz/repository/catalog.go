// Package repository defines OKZ persistence contracts and PostgreSQL adapter.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"

	"cybercalc/internal/modules/okz/domain"
	platformaudit "cybercalc/internal/platform/audit"
)

var (
	ErrNoActiveVersion = errors.New("active OKZ version is not configured")
	ErrVersionExists   = errors.New("OKZ version already exists")
)

type Catalog interface {
	Search(context.Context, string, int, int, int) (domain.SearchPage, error)
	ListVersions(context.Context) ([]domain.Version, error)
	Import(context.Context, domain.Import) (domain.Version, error)
}

type SQLCatalog struct {
	db *sql.DB
}

func NewSQLCatalog(db *sql.DB) *SQLCatalog { return &SQLCatalog{db: db} }

func (c *SQLCatalog) Search(ctx context.Context, query string, level, limit, offset int) (domain.SearchPage, error) {
	if c.db == nil {
		return domain.SearchPage{}, errors.New("database is not configured")
	}
	var versionID, version string
	if err := c.db.QueryRowContext(ctx, `SELECT id,version FROM okz_catalog_versions WHERE status='active'`).Scan(&versionID, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.SearchPage{Items: []domain.Occupation{}, Limit: limit, Offset: offset}, ErrNoActiveVersion
		}
		return domain.SearchPage{}, err
	}
	escaped := escapeLike(query)
	var total int
	if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM okz_occupations
		WHERE version_id=$1 AND ($2='' OR code LIKE $2 || '%' ESCAPE '\' OR name ILIKE '%' || $2 || '%' ESCAPE '\')
		AND ($3=0 OR level=$3)`, versionID, escaped, level).Scan(&total); err != nil {
		return domain.SearchPage{}, err
	}
	rows, err := c.db.QueryContext(ctx, `SELECT code,level,COALESCE(parent_code,''),name FROM okz_occupations
		WHERE version_id=$1 AND ($2='' OR code LIKE $2 || '%' ESCAPE '\' OR name ILIKE '%' || $2 || '%' ESCAPE '\')
		AND ($3=0 OR level=$3) ORDER BY code LIMIT $4 OFFSET $5`, versionID, escaped, level, limit, offset)
	if err != nil {
		return domain.SearchPage{}, err
	}
	defer rows.Close()
	items := make([]domain.Occupation, 0)
	for rows.Next() {
		var item domain.Occupation
		if err := rows.Scan(&item.Code, &item.Level, &item.ParentCode, &item.Name); err != nil {
			return domain.SearchPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return domain.SearchPage{}, err
	}
	return domain.SearchPage{Items: items, Total: total, Limit: limit, Offset: offset, Version: version}, nil
}

func (c *SQLCatalog) ListVersions(ctx context.Context) ([]domain.Version, error) {
	if c.db == nil {
		return nil, errors.New("database is not configured")
	}
	rows, err := c.db.QueryContext(ctx, `SELECT v.id,v.version,v.source_name,v.source_url,v.effective_on,v.status,
		count(o.code),v.imported_at,v.activated_at
		FROM okz_catalog_versions v LEFT JOIN okz_occupations o ON o.version_id=v.id
		GROUP BY v.id ORDER BY (v.status='active') DESC,v.effective_on DESC,v.imported_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := make([]domain.Version, 0)
	for rows.Next() {
		var item domain.Version
		var effectiveOn time.Time
		var importedAt, activatedAt time.Time
		if err := rows.Scan(&item.ID, &item.Version, &item.SourceName, &item.SourceURL, &effectiveOn,
			&item.Status, &item.ItemCount, &importedAt, &activatedAt); err != nil {
			return nil, err
		}
		item.EffectiveOn = effectiveOn.Format("2006-01-02")
		item.ImportedAt = importedAt.UTC().Format(time.RFC3339)
		item.ActivatedAt = activatedAt.UTC().Format(time.RFC3339)
		versions = append(versions, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

func (c *SQLCatalog) Import(ctx context.Context, input domain.Import) (domain.Version, error) {
	if c.db == nil {
		return domain.Version{}, errors.New("database is not configured")
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Version{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('okz_catalog_import'))`); err != nil {
		return domain.Version{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE okz_catalog_versions SET status='archived' WHERE status='active'`); err != nil {
		return domain.Version{}, err
	}
	var id string
	var importedAt, activatedAt time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO okz_catalog_versions
		(version,source_name,source_url,effective_on,status,imported_by)
		VALUES($1,$2,$3,$4,'active',$5) RETURNING id,imported_at,activated_at`,
		input.Version, input.SourceName, input.SourceURL, input.EffectiveOn, input.ImportedBy).
		Scan(&id, &importedAt, &activatedAt)
	if err != nil {
		var databaseError *pq.Error
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return domain.Version{}, ErrVersionExists
		}
		return domain.Version{}, err
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO okz_occupations(version_id,code,name) VALUES($1,$2,$3)`)
	if err != nil {
		return domain.Version{}, err
	}
	defer statement.Close()
	for _, record := range input.Records {
		if _, err := statement.ExecContext(ctx, id, record.Code, record.Name); err != nil {
			return domain.Version{}, fmt.Errorf("insert OKZ %s: %w", record.Code, err)
		}
	}
	if err := platformaudit.Write(ctx, tx, platformaudit.Event{
		Actor:  platformaudit.UserActor(input.ImportedBy),
		Action: "okz_import",
		Entity: platformaudit.Entity{Type: "okz_catalog_version", ID: id},
		After: map[string]any{
			"version": input.Version, "records": len(input.Records), "effective_on": input.EffectiveOn,
		},
	}); err != nil {
		return domain.Version{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Version{}, err
	}
	return domain.Version{
		ID: id, Version: input.Version, SourceName: input.SourceName, SourceURL: input.SourceURL,
		EffectiveOn: input.EffectiveOn, Status: "active", ItemCount: len(input.Records),
		ImportedAt: importedAt.UTC().Format(time.RFC3339), ActivatedAt: activatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
