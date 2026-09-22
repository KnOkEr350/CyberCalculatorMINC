// Package repository defines normative source persistence contracts.
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"cybercalc/internal/modules/normative/domain"
	platformaudit "cybercalc/internal/platform/audit"
	"github.com/lib/pq"
)

var (
	ErrRevisionConflict      = errors.New("normative revision already has different content")
	ErrEffectiveDateConflict = errors.New("normative effective date already belongs to another revision")
	ErrNotFound              = errors.New("normative revision not found")
)

type Registry interface {
	IsTrustedHost(context.Context, string) (bool, error)
	List(context.Context, string) ([]domain.Source, error)
	Import(context.Context, domain.Import) (domain.Source, *domain.DiffProtocol, error)
	Find(context.Context, string, string) (domain.Source, error)
}

type SQLRegistry struct{ db *sql.DB }

func NewSQLRegistry(db *sql.DB) *SQLRegistry { return &SQLRegistry{db: db} }

func (r *SQLRegistry) IsTrustedHost(ctx context.Context, host string) (bool, error) {
	var trusted bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normative_trusted_hosts WHERE host=$1)`, host).Scan(&trusted)
	return trusted, err
}

func (r *SQLRegistry) List(ctx context.Context, actCode string) ([]domain.Source, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id::text,act_code,title,revision,published_on,effective_on,source_url,source_host,
		content_sha256,content_type,original_filename,size_bytes,imported_by::text,imported_at
		FROM normative_sources WHERE ($1='' OR act_code=$1) ORDER BY act_code,effective_on DESC,imported_at DESC`, actCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domain.Source, 0)
	for rows.Next() {
		item, err := scanSource(rows, false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SQLRegistry) Find(ctx context.Context, actCode, revision string) (domain.Source, error) {
	item, err := scanSource(r.db.QueryRowContext(ctx, `SELECT id::text,act_code,title,revision,published_on,effective_on,source_url,source_host,
		content_sha256,content_type,original_filename,size_bytes,imported_by::text,imported_at,content_bytes
		FROM normative_sources WHERE act_code=$1 AND revision=$2`, actCode, revision), true)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Source{}, ErrNotFound
	}
	return item, err
}

func (r *SQLRegistry) Import(ctx context.Context, input domain.Import) (domain.Source, *domain.DiffProtocol, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Source{}, nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "normative:"+input.ActCode); err != nil {
		return domain.Source{}, nil, err
	}
	existing, err := scanSource(tx.QueryRowContext(ctx, `SELECT id::text,act_code,title,revision,published_on,effective_on,source_url,source_host,
		content_sha256,content_type,original_filename,size_bytes,imported_by::text,imported_at,content_bytes
		FROM normative_sources WHERE act_code=$1 AND revision=$2`, input.ActCode, input.Revision), true)
	if err == nil {
		if !sameImport(existing, input) {
			return domain.Source{}, nil, ErrRevisionConflict
		}
		return existing, nil, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.Source{}, nil, err
	}
	var dateConflict bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM normative_sources WHERE act_code=$1 AND effective_on=$2)`, input.ActCode, input.EffectiveOn).Scan(&dateConflict); err != nil {
		return domain.Source{}, nil, err
	}
	if dateConflict {
		return domain.Source{}, nil, ErrEffectiveDateConflict
	}
	previous, previousErr := scanSource(tx.QueryRowContext(ctx, `SELECT id::text,act_code,title,revision,published_on,effective_on,source_url,source_host,
		content_sha256,content_type,original_filename,size_bytes,imported_by::text,imported_at,content_bytes
		FROM normative_sources WHERE act_code=$1 AND effective_on<$2 ORDER BY effective_on DESC,imported_at DESC LIMIT 1`, input.ActCode, input.EffectiveOn), true)
	if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
		return domain.Source{}, nil, previousErr
	}
	var source domain.Source
	var published sql.NullTime
	if input.PublishedOn != "" {
		parsed, parseErr := time.Parse("2006-01-02", input.PublishedOn)
		if parseErr != nil {
			return domain.Source{}, nil, parseErr
		}
		published = sql.NullTime{Time: parsed, Valid: true}
	}
	var effective, imported time.Time
	err = tx.QueryRowContext(ctx, `INSERT INTO normative_sources(
		act_code,title,revision,published_on,effective_on,source_url,source_host,content_sha256,content_type,
		original_filename,size_bytes,content_bytes,imported_by
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::uuid)
	RETURNING id::text,effective_on,imported_at`, input.ActCode, input.Title, input.Revision, published, input.EffectiveOn,
		input.SourceURL, input.SourceHost, input.ExpectedSHA256, input.ContentType, input.OriginalFilename, len(input.Content), input.Content, input.ImportedBy).
		Scan(&source.ID, &effective, &imported)
	if err != nil {
		var databaseError *pq.Error
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return domain.Source{}, nil, ErrRevisionConflict
		}
		return domain.Source{}, nil, err
	}
	source.ActCode, source.Title, source.Revision = input.ActCode, input.Title, input.Revision
	source.PublishedOn, source.EffectiveOn = input.PublishedOn, effective.Format("2006-01-02")
	source.SourceURL, source.SourceHost, source.ContentSHA256 = input.SourceURL, input.SourceHost, input.ExpectedSHA256
	source.ContentType, source.OriginalFilename, source.SizeBytes = input.ContentType, input.OriginalFilename, len(input.Content)
	source.ImportedBy, source.Content = input.ImportedBy, input.Content
	source.ImportedAt = imported.UTC().Format(time.RFC3339)
	var protocol *domain.DiffProtocol
	if previousErr == nil {
		value := domain.Compare(previous, source)
		encoded, _ := json.Marshal(value)
		if _, err := tx.ExecContext(ctx, `INSERT INTO normative_revision_diffs(act_code,from_source_id,to_source_id,protocol) VALUES($1,$2,$3,$4)`, input.ActCode, previous.ID, source.ID, encoded); err != nil {
			return domain.Source{}, nil, err
		}
		protocol = &value
	}
	if err := platformaudit.Write(ctx, tx, platformaudit.Event{Actor: platformaudit.UserActor(input.ImportedBy), Action: "normative_source_import", Entity: platformaudit.Entity{Type: "normative_source", ID: source.ID}, After: map[string]any{"act_code": source.ActCode, "revision": source.Revision, "effective_on": source.EffectiveOn, "sha256": source.ContentSHA256, "source_url": source.SourceURL}}); err != nil {
		return domain.Source{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Source{}, nil, err
	}
	return source, protocol, nil
}

func sameImport(existing domain.Source, input domain.Import) bool {
	return existing.ActCode == input.ActCode &&
		existing.Title == input.Title &&
		existing.Revision == input.Revision &&
		existing.PublishedOn == input.PublishedOn &&
		existing.EffectiveOn == input.EffectiveOn &&
		existing.SourceURL == input.SourceURL &&
		existing.SourceHost == input.SourceHost &&
		existing.ContentSHA256 == input.ExpectedSHA256 &&
		existing.ContentType == input.ContentType &&
		existing.OriginalFilename == input.OriginalFilename
}

type scanner interface{ Scan(...any) error }

func scanSource(row scanner, withContent bool) (domain.Source, error) {
	var item domain.Source
	var published sql.NullTime
	var effective, imported time.Time
	values := []any{&item.ID, &item.ActCode, &item.Title, &item.Revision, &published, &effective, &item.SourceURL, &item.SourceHost, &item.ContentSHA256, &item.ContentType, &item.OriginalFilename, &item.SizeBytes, &item.ImportedBy, &imported}
	if withContent {
		values = append(values, &item.Content)
	}
	if err := row.Scan(values...); err != nil {
		return domain.Source{}, err
	}
	if published.Valid {
		item.PublishedOn = published.Time.Format("2006-01-02")
	}
	item.EffectiveOn = effective.Format("2006-01-02")
	item.ImportedAt = imported.UTC().Format(time.RFC3339)
	return item, nil
}
