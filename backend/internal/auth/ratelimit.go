package auth

import (
	"context"
	"database/sql"
)

// AllowAttempt shares counters across replicas; keys contain no email or IP.
func AllowAttempt(ctx context.Context, db *sql.DB, identity string, limit int) (bool, error) {
	var hits int
	err := db.QueryRowContext(ctx, `INSERT INTO auth_rate_limits(key,hits,expires_at)
		VALUES($1,1,now()+interval '15 minutes') ON CONFLICT(key) DO UPDATE SET
		hits=CASE WHEN auth_rate_limits.expires_at<=now() THEN 1 ELSE LEAST(auth_rate_limits.hits+1,1000000) END,
		expires_at=CASE WHEN auth_rate_limits.expires_at<=now() THEN now()+interval '15 minutes' ELSE auth_rate_limits.expires_at END
		RETURNING hits`, TokenHash(identity)).Scan(&hits)
	return hits <= limit, err
}
