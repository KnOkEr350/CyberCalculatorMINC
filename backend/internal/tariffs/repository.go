// Package tariffs loads the versioned Order 270 tariff selected for a report
// year. The database owns rates and provenance; calculators own arithmetic.
package tariffs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNoActiveVersion = errors.New("нет действующей версии тарифов")
var ErrUnverifiedProvenance = errors.New("версия тарифов не связана с проверенным нормативным источником")

type Version struct {
	ID                 string            `json:"id"`
	Code               string            `json:"code"`
	Title              string            `json:"title"`
	EffectiveFrom      string            `json:"effective_from"`
	EffectiveUntil     string            `json:"effective_until,omitempty"`
	SourceReference    string            `json:"source_reference"`
	ProvenanceVerified bool              `json:"provenance_verified"`
	Rates              map[string]string `json:"rates"`
}

type Repository struct{ DB *sql.DB }

func (r Repository) Active(ctx context.Context, reportYear int, activity, audience string) (Version, error) {
	if reportYear < 2000 || reportYear > 2100 {
		return Version{}, fmt.Errorf("некорректный отчётный год")
	}
	date := time.Date(reportYear, time.January, 1, 0, 0, 0, 0, time.UTC)
	var version Version
	var effectiveFrom time.Time
	var effectiveUntil sql.NullTime
	err := r.DB.QueryRowContext(ctx, `SELECT id::text,code,title,effective_from,effective_until,source_reference,provenance_verified
		FROM tariff_versions WHERE status='active' AND calculation_mode='average'
		AND effective_from<=$1 AND (effective_until IS NULL OR effective_until>=$1)
		ORDER BY effective_from DESC,id LIMIT 1`, date).Scan(&version.ID, &version.Code, &version.Title,
		&effectiveFrom, &effectiveUntil, &version.SourceReference, &version.ProvenanceVerified)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, ErrNoActiveVersion
	}
	if err != nil {
		return Version{}, err
	}
	version.EffectiveFrom = effectiveFrom.Format("2006-01-02")
	if effectiveUntil.Valid {
		version.EffectiveUntil = effectiveUntil.Time.Format("2006-01-02")
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT component_code,rate_rub::text FROM tariff_rules
		WHERE tariff_version_id=$1 AND activity_code=$2 AND audience IN ($3,'all')
		ORDER BY (audience=$3)`, version.ID, activity, audience)
	if err != nil {
		return Version{}, err
	}
	defer rows.Close()
	version.Rates = map[string]string{}
	for rows.Next() {
		var component, rate string
		if err := rows.Scan(&component, &rate); err != nil {
			return Version{}, err
		}
		version.Rates[component] = rate
	}
	if err := rows.Err(); err != nil {
		return Version{}, err
	}
	if len(version.Rates) == 0 {
		return Version{}, fmt.Errorf("%w: %s/%s", ErrNoActiveVersion, activity, audience)
	}
	return version, nil
}
