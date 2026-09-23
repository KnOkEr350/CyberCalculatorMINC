// Package curators ведёт закрепление кураторов за партнёрами (DATA-09).
//
// Источник истины — таблица user_partner_assignments: период, отзыв, история и
// граница арендатора проверяются на уровне БД. Здесь — операции над ней и
// выбор куратора для fallback (SEC-04).
package curators

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNotFound        = errors.New("закрепление не найдено")
	ErrOverlap         = errors.New("в этот период куратор уже закреплён за партнёром")
	ErrNotCurator      = errors.New("закреплять можно только куратора организации")
	ErrForeignTenant   = errors.New("куратор и партнёр относятся к разным ИТ-компаниям")
	ErrInvalidPeriod   = errors.New("окончание периода раньше начала")
	ErrReasonRequired  = errors.New("укажите причину")
	ErrAlreadyRevoked  = errors.New("закрепление уже отозвано")
	ErrNothingToChange = errors.New("окончание не сдвигает период")
)

// BusinessLocation — московское время: календарные даты закрепления считаются
// по нему, как и сроки регламента.
var BusinessLocation = func() *time.Location {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*60*60)
	}
	return location
}()

// Today — московская дата на момент now.
func Today(now time.Time) time.Time {
	year, month, day := now.In(BusinessLocation).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Assignment — закрепление.
type Assignment struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	PartnerID   string `json:"partner_id"`
	ITCompanyID string `json:"it_company_id,omitempty"`
	ValidFrom   string `json:"valid_from"`
	ValidUntil  string `json:"valid_until,omitempty"`
	AssignedBy  string `json:"assigned_by,omitempty"`
	Reason      string `json:"reason,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	RevokedBy   string `json:"revoked_by,omitempty"`
	RevokeNote  string `json:"revoke_reason,omitempty"`
	Active      bool   `json:"active"`
}

const columns = `id::text,user_id::text,partner_id::text,COALESCE(it_company_id::text,''),valid_from::text,
	COALESCE(valid_until::text,''),COALESCE(assigned_by::text,''),COALESCE(reason,''),
	COALESCE(revoked_at::text,''),COALESCE(revoked_by::text,''),COALESCE(revoke_reason,'')`

func scan(row interface{ Scan(...any) error }, today string) (Assignment, error) {
	var a Assignment
	if err := row.Scan(&a.ID, &a.UserID, &a.PartnerID, &a.ITCompanyID, &a.ValidFrom, &a.ValidUntil,
		&a.AssignedBy, &a.Reason, &a.RevokedAt, &a.RevokedBy, &a.RevokeNote); err != nil {
		return a, err
	}
	a.Active = a.RevokedAt == "" && a.ValidFrom <= today && (a.ValidUntil == "" || a.ValidUntil >= today)
	return a, nil
}

// NewInput — параметры нового закрепления.
type NewInput struct {
	CuratorID  string
	PartnerID  string
	From       time.Time
	Until      *time.Time // nil — без окончания
	AssignedBy string
	Reason     string
}

func classify(err error) error {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23P01":
		return ErrOverlap
	case pgErr.Code == "P0001" && strings.Contains(pgErr.Message, "only curators"):
		return ErrNotCurator
	case pgErr.Code == "P0001" && strings.Contains(pgErr.Message, "same IT company"):
		return ErrForeignTenant
	case pgErr.Code == "23514":
		return ErrInvalidPeriod
	}
	return err
}

// Assign закрепляет куратора за партнёром на период. Пересечение с другим
// закреплением того же куратора и чужой арендатор отвергаются БД.
func Assign(ctx context.Context, db queryer, in NewInput, now time.Time) (Assignment, error) {
	if in.Until != nil && in.Until.Before(in.From) {
		return Assignment{}, ErrInvalidPeriod
	}
	var until interface{}
	if in.Until != nil {
		until = in.Until.Format("2006-01-02")
	}
	var by interface{}
	if in.AssignedBy != "" {
		by = in.AssignedBy
	}
	row := db.QueryRowContext(ctx, `INSERT INTO user_partner_assignments(user_id,partner_id,valid_from,valid_until,assigned_by,reason)
		VALUES($1::uuid,$2::uuid,$3::date,$4::date,$5::uuid,NULLIF(btrim($6),'')) RETURNING `+columns,
		in.CuratorID, in.PartnerID, in.From.Format("2006-01-02"), until, by, in.Reason)
	a, err := scan(row, Today(now).Format("2006-01-02"))
	if err != nil {
		return Assignment{}, classify(err)
	}
	return a, nil
}

// End заканчивает действующее или будущее закрепление указанным днём
// включительно. Период можно только сократить: продление — новое закрепление,
// иначе оно молча пересекло бы соседнее.
func End(ctx context.Context, db queryer, id string, until time.Time, now time.Time) (Assignment, error) {
	current, err := Get(ctx, db, id, now)
	if err != nil {
		return Assignment{}, err
	}
	if current.RevokedAt != "" {
		return Assignment{}, ErrAlreadyRevoked
	}
	day := until.Format("2006-01-02")
	if day < current.ValidFrom {
		return Assignment{}, ErrInvalidPeriod
	}
	if current.ValidUntil != "" && day >= current.ValidUntil {
		return Assignment{}, ErrNothingToChange
	}
	row := db.QueryRowContext(ctx, `UPDATE user_partner_assignments SET valid_until=$2::date WHERE id::text=$1 RETURNING `+columns, id, day)
	a, err := scan(row, Today(now).Format("2006-01-02"))
	if err != nil {
		return Assignment{}, classify(err)
	}
	return a, nil
}

// Revoke отзывает закрепление: оно не действует ни в один день. Причина
// обязательна — отзыв стирает период из зачёта, и это должно быть объяснимо.
func Revoke(ctx context.Context, db queryer, id, by, reason string, now time.Time) (Assignment, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Assignment{}, ErrReasonRequired
	}
	current, err := Get(ctx, db, id, now)
	if err != nil {
		return Assignment{}, err
	}
	if current.RevokedAt != "" {
		return Assignment{}, ErrAlreadyRevoked
	}
	var actor interface{}
	if by != "" {
		actor = by
	}
	row := db.QueryRowContext(ctx, `UPDATE user_partner_assignments SET revoked_at=now(),revoked_by=$2::uuid,revoke_reason=$3
		WHERE id::text=$1 RETURNING `+columns, id, actor, reason)
	a, err := scan(row, Today(now).Format("2006-01-02"))
	if err != nil {
		return Assignment{}, classify(err)
	}
	return a, nil
}

// Get читает закрепление.
func Get(ctx context.Context, db queryer, id string, now time.Time) (Assignment, error) {
	a, err := scan(db.QueryRowContext(ctx, `SELECT `+columns+` FROM user_partner_assignments WHERE id::text=$1`, id),
		Today(now).Format("2006-01-02"))
	if errors.Is(err, sql.ErrNoRows) {
		return Assignment{}, ErrNotFound
	}
	return a, err
}

// Filter ограничивает список.
type Filter struct {
	CuratorID   string
	PartnerID   string
	ITCompanyID string // пусто — без ограничения арендатором
	ActiveOnly  bool
}

// List возвращает закрепления, новые первыми.
func List(ctx context.Context, db queryer, f Filter, now time.Time) ([]Assignment, error) {
	today := Today(now).Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `SELECT `+columns+` FROM user_partner_assignments
		WHERE ($1='' OR user_id::text=$1) AND ($2='' OR partner_id::text=$2) AND ($3='' OR it_company_id::text=$3)
		  AND (NOT $4 OR (revoked_at IS NULL AND valid_from<=$5::date AND (valid_until IS NULL OR valid_until>=$5::date)))
		ORDER BY valid_from DESC,created_at DESC,id LIMIT 500`, f.CuratorID, f.PartnerID, f.ITCompanyID, f.ActiveOnly, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		a, err := scan(rows, today)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ForPartner возвращает идентификаторы кураторов, действующих сейчас на
// партнёре: активных пользователей, чьё закрепление действует на дату.
func ForPartner(ctx context.Context, db queryer, partnerID string, now time.Time) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT a.user_id::text FROM user_partner_assignments a JOIN users u ON u.id=a.user_id
		WHERE a.partner_id::text=$1 AND a.revoked_at IS NULL AND u.is_active AND u.role='curator' AND u.entity_type='organization'
		  AND a.valid_from<=$2::date AND (a.valid_until IS NULL OR a.valid_until>=$2::date)
		ORDER BY a.valid_from,a.user_id`, partnerID, Today(now).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Sync приводит users.partner_id кураторов в соответствие с закреплениями на
// дату: периоды начинаются и кончаются сами по себе, без события в БД.
// Возвращает число исправленных профилей. Повторный запуск ничего не меняет.
func Sync(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Пересчёт — не «прямое изменение профиля»: без метки триггер users
	// принял бы его за новое закрепление и стал бы менять периоды.
	if _, err := tx.ExecContext(ctx, `SELECT set_config('curators.sync','on',true)`); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE users u SET partner_id=current_partner_of(u.id,$1::date),updated_at=now()
		WHERE u.role='curator' AND u.entity_type='organization'
		  AND u.partner_id IS DISTINCT FROM current_partner_of(u.id,$1::date)`, Today(now).Format("2006-01-02"))
	if err != nil {
		return 0, fmt.Errorf("пересчитать закрепления кураторов: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return changed, tx.Commit()
}

// RunSync запускает Sync по расписанию до закрытия stop.
func RunSync(db *sql.DB, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_, _ = Sync(ctx, db, time.Now())
		cancel()
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}
