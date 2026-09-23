package regulatory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
)

var (
	ErrNotFound = errors.New("процесс не найден")
	ErrExists   = errors.New("процесс этого вида уже создан для соглашения и года")
)

// Process — сохранённый процесс: состояние и его адрес.
type Process struct {
	ID          string
	AgreementID string
	State       State
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StoredEvent — переход из истории процесса.
type StoredEvent struct {
	Event
	ActorID string
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const processColumns = `id::text,kind,agreement_id::text,report_year,status,COALESCE(window_kind,''),
	COALESCE(due_date::text,''),expires_at,round,sent_at,sent_late,COALESCE(remarks,''),rework_lapsed_at,created_at,updated_at`

func scanProcess(row interface{ Scan(...any) error }) (Process, error) {
	var p Process
	var kind, status, window string
	var expires, sent, lapsed sql.NullTime
	if err := row.Scan(&p.ID, &kind, &p.AgreementID, &p.State.ReportYear, &status, &window,
		&p.State.DueDate, &expires, &p.State.Round, &sent, &p.State.SentLate, &p.State.Remarks, &lapsed,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return Process{}, err
	}
	p.State.Kind, p.State.Status, p.State.Window = Kind(kind), Status(status), Window(window)
	if expires.Valid {
		p.State.ExpiresAt = expires.Time
	}
	if sent.Valid {
		t := sent.Time
		p.State.SentAt = &t
	}
	if lapsed.Valid {
		t := lapsed.Time
		p.State.ReworkLapsedAt = &t
	}
	return p, nil
}

// Create заводит процесс в состоянии «черновик». Повторное создание того же
// вида для того же соглашения и года возвращает ErrExists вместе с уже
// существующим процессом: вызов идемпотентен.
func Create(ctx context.Context, db queryer, kind Kind, agreementID string, reportYear int, createdBy string) (Process, error) {
	if _, err := New(kind, reportYear); err != nil {
		return Process{}, err
	}
	row := db.QueryRowContext(ctx, `INSERT INTO regulatory_processes(kind,agreement_id,report_year,status,round,created_by)
		VALUES($1,$2::uuid,$3,'draft',0,$4::uuid)
		ON CONFLICT (kind,agreement_id,report_year) DO NOTHING
		RETURNING `+processColumns, string(kind), agreementID, reportYear, createdBy)
	process, err := scanProcess(row)
	if err == nil {
		return process, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Process{}, err
	}
	existing, err := scanProcess(db.QueryRowContext(ctx, `SELECT `+processColumns+`
		FROM regulatory_processes WHERE kind=$1 AND agreement_id::text=$2 AND report_year=$3`,
		string(kind), agreementID, reportYear))
	if err != nil {
		return Process{}, err
	}
	return existing, ErrExists
}

// Get читает процесс.
func Get(ctx context.Context, db queryer, id string) (Process, error) {
	process, err := scanProcess(db.QueryRowContext(ctx, `SELECT `+processColumns+` FROM regulatory_processes WHERE id::text=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Process{}, ErrNotFound
	}
	return process, err
}

// Events возвращает историю переходов в порядке их совершения.
func Events(ctx context.Context, db queryer, id string) ([]StoredEvent, error) {
	rows, err := db.QueryContext(ctx, `SELECT action,from_status,to_status,COALESCE(reason,''),COALESCE(due_date::text,''),
			COALESCE(actor_id::text,''),system_action,occurred_at
		FROM regulatory_process_events WHERE process_id::text=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StoredEvent{}
	for rows.Next() {
		var e StoredEvent
		var action, from, to string
		if err := rows.Scan(&action, &from, &to, &e.Reason, &e.DueDate, &e.ActorID, &e.System, &e.At); err != nil {
			return nil, err
		}
		e.Action, e.From, e.To = Action(action), Status(from), Status(to)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Act выполняет действие в одной транзакции. Процесс блокируется на время
// перехода: два одновременных ответа рецензента не могут оба «выиграть».
// Перед действием применяется согласование по молчанию — иначе действие
// пришло бы в процесс, который уже согласован истёкшим сроком.
//
// hook, если задан, выполняется в той же транзакции после записи перехода:
// журнал аудита пишется атомарно с переходом, а сбой хука откатывает и его.
func Act(ctx context.Context, db *sql.DB, id string, action Action, actorID, reason string, now time.Time,
	hook func(context.Context, *sql.Tx, []StoredEvent) error) (Process, []StoredEvent, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Process{}, nil, err
	}
	defer tx.Rollback()

	process, err := scanProcess(tx.QueryRowContext(ctx, `SELECT `+processColumns+`
		FROM regulatory_processes WHERE id::text=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Process{}, nil, ErrNotFound
	}
	if err != nil {
		return Process{}, nil, err
	}

	var applied []StoredEvent
	// Сначала — то, что произошло само: истёкший срок.
	expired, expiryEvent := ExpireIfDue(process.State, now)
	if expiryEvent != nil {
		if err := persist(ctx, tx, process.ID, expired, *expiryEvent, ""); err != nil {
			return Process{}, nil, err
		}
		applied = append(applied, StoredEvent{Event: *expiryEvent})
		process.State = expired
	}
	next, event, err := Apply(process.State, action, Input{Now: now, Reason: reason})
	if err != nil {
		// Согласование по молчанию, случившееся по дороге, сохраняется даже
		// если само действие отклонено: срок истёк независимо от него.
		if expiryEvent != nil {
			if commitErr := tx.Commit(); commitErr != nil {
				return Process{}, nil, commitErr
			}
			process, _ = Get(ctx, db, id)
			return process, applied, err
		}
		return process, nil, err
	}
	if err := persist(ctx, tx, process.ID, next, event, actorID); err != nil {
		return Process{}, nil, err
	}
	applied = append(applied, StoredEvent{Event: event, ActorID: actorID})
	if hook != nil {
		if err := hook(ctx, tx, applied); err != nil {
			return Process{}, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Process{}, nil, err
	}
	process.State = next
	process.UpdatedAt = now
	return process, applied, nil
}

func persist(ctx context.Context, db queryer, id string, state State, event Event, actorID string) error {
	var window, due, expires, sent, lapsed interface{}
	if state.Window != WindowNone {
		window, due, expires = string(state.Window), state.DueDate, state.ExpiresAt
	}
	if state.SentAt != nil {
		sent = *state.SentAt
	}
	if state.ReworkLapsedAt != nil {
		lapsed = *state.ReworkLapsedAt
	}
	if _, err := db.ExecContext(ctx, `UPDATE regulatory_processes SET status=$2,window_kind=$3,due_date=$4::date,expires_at=$5,
			round=$6,sent_at=$7,sent_late=$8,remarks=NULLIF($9,''),rework_lapsed_at=$10,updated_at=now() WHERE id::text=$1`,
		id, string(state.Status), window, due, expires, state.Round, sent, state.SentLate, state.Remarks, lapsed); err != nil {
		return fmt.Errorf("сохранить состояние процесса: %w", err)
	}
	var actor interface{}
	if actorID != "" {
		actor = actorID
	}
	if event.System {
		actor = nil
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO regulatory_process_events(process_id,action,from_status,to_status,reason,due_date,actor_id,system_action)
		VALUES($1::uuid,$2,$3,$4,NULLIF($5,''),NULLIF($6,'')::date,$7::uuid,$8)`,
		id, string(event.Action), string(event.From), string(event.To), event.Reason, event.DueDate, actor, event.System); err != nil {
		return fmt.Errorf("записать переход процесса: %w", err)
	}
	return nil
}

// ExpireDue проходит по процессам с истёкшим окном и применяет согласование по
// молчанию. Возвращает число переведённых в «согласован по молчанию» и число
// зафиксированных пропусков доработки. Каждый процесс обрабатывается в своей
// транзакции с блокировкой строки, поэтому параллельные экземпляры задания и
// действия пользователей не мешают друг другу, а повторный запуск ничего не
// меняет.
//
// agreementIDs ограничивает проход процессами этих соглашений; в работе не
// задаётся — задание обходит все. Ограничение нужно проверкам на общей базе,
// чтобы проход одного теста не переводил чужие процессы.
func ExpireDue(ctx context.Context, db *sql.DB, now time.Time, agreementIDs ...string) (approved, lapsed int, err error) {
	rows, err := db.QueryContext(ctx, `SELECT id::text FROM regulatory_processes
		WHERE window_kind IS NOT NULL AND expires_at <= $1
		  AND NOT (window_kind='rework' AND rework_lapsed_at IS NOT NULL)
		  AND (cardinality($2::uuid[])=0 OR agreement_id = ANY($2::uuid[]))
		ORDER BY expires_at,id LIMIT 500`, now, pq.Array(agreementIDs))
	if err != nil {
		return 0, 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return approved, lapsed, ctx.Err()
		}
		outcome, err := expireOne(ctx, db, id, now)
		if err != nil {
			return approved, lapsed, fmt.Errorf("процесс %s: %w", id, err)
		}
		switch outcome {
		case ActionDefaultApprove:
			approved++
		case ActionReworkLapsed:
			lapsed++
		}
	}
	return approved, lapsed, nil
}

func expireOne(ctx context.Context, db *sql.DB, id string, now time.Time) (Action, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	process, err := scanProcess(tx.QueryRowContext(ctx, `SELECT `+processColumns+`
		FROM regulatory_processes WHERE id::text=$1 FOR UPDATE`, id))
	if err != nil {
		return "", err
	}
	next, event := ExpireIfDue(process.State, now)
	if event == nil {
		return "", nil
	}
	if err := persist(ctx, tx, id, next, *event, ""); err != nil {
		return "", err
	}
	return event.Action, tx.Commit()
}
