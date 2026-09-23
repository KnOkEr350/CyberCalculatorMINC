package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/models"
	platformaudit "cybercalc/internal/platform/audit"
	"cybercalc/internal/rbac"
	"github.com/lib/pq"
)

var (
	ErrNotFound      = errors.New("задача не найдена")
	ErrNotOpen       = errors.New("задача уже закрыта")
	ErrInvalid       = errors.New("некорректная задача")
	ErrNotAssignee   = errors.New("задачу выполняет её исполнитель")
	ErrNotDelegated  = errors.New("это действие куратору не делегировано")
	ErrPartnerTenant = errors.New("партнёр не относится к ИТ-компании задачи")
)

// Task — сохранённая задача.
type Task struct {
	ID                 string `json:"id"`
	ITCompanyID        string `json:"it_company_id"`
	PartnerID          string `json:"partner_id,omitempty"`
	Kind               string `json:"kind"`
	Title              string `json:"title"`
	SubjectType        string `json:"subject_type"`
	SubjectID          string `json:"subject_id"`
	RequiredRole       string `json:"required_role"`
	RequiredPermission string `json:"required_permission"`
	Status             string `json:"status"`
	AssigneeID         string `json:"assignee_id,omitempty"`
	Route              Route  `json:"route"`
	Escalated          bool   `json:"unassigned_escalated"`
	FallbackReason     string `json:"fallback_reason,omitempty"`
	CreatedAt          string `json:"created_at"`
	CompletedAt        string `json:"completed_at,omitempty"`
}

// Event — запись истории задачи.
type Event struct {
	Action       string `json:"action"`
	FromAssignee string `json:"from_assignee,omitempty"`
	ToAssignee   string `json:"to_assignee,omitempty"`
	Route        string `json:"route"`
	Reason       string `json:"reason,omitempty"`
	ActorID      string `json:"actor_id,omitempty"`
	System       bool   `json:"system"`
	OccurredAt   string `json:"occurred_at"`
}

const taskColumns = `id::text,it_company_id::text,COALESCE(partner_id::text,''),kind,title,subject_type,subject_id,
	required_role,required_permission,status,COALESCE(assignee_id::text,''),route,unassigned_escalated,
	COALESCE(fallback_reason,''),created_at::text,COALESCE(completed_at::text,'')`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	var route string
	err := row.Scan(&t.ID, &t.ITCompanyID, &t.PartnerID, &t.Kind, &t.Title, &t.SubjectType, &t.SubjectID,
		&t.RequiredRole, &t.RequiredPermission, &t.Status, &t.AssigneeID, &route, &t.Escalated,
		&t.FallbackReason, &t.CreatedAt, &t.CompletedAt)
	t.Route = Route(route)
	return t, err
}

// requirement собирает требование к исполнителю из задачи.
func (t Task) requirement() Requirement {
	return Requirement{Role: models.Role(t.RequiredRole), Permission: rbac.Permission(t.RequiredPermission), HasPartner: t.PartnerID != ""}
}

// candidates читает активных пользователей для каждого шага цепочки. Внутри
// шага предпочтение у того, у кого меньше открытых задач, затем — по
// идентификатору: раздача детерминирована и не копит всё на одном человеке.
func candidates(ctx context.Context, db queryer, t Task, now time.Time) (Candidates, error) {
	var c Candidates
	byRole := func(role string, tenantOnly bool) ([]string, error) {
		rows, err := db.QueryContext(ctx, `SELECT u.id::text FROM users u
			WHERE u.is_active AND u.role=$1 AND u.entity_type='organization'
			  AND (u.it_company_id::text=$2 OR (NOT $3 AND u.it_company_id IS NULL))
			ORDER BY (u.it_company_id IS NULL),
			  (SELECT count(*) FROM workflow_tasks w WHERE w.assignee_id=u.id AND w.status='open'),u.id`, role, t.ITCompanyID, tenantOnly)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	var err error
	if t.PartnerID != "" {
		if c.Curators, err = curators.ForPartner(ctx, db, t.PartnerID, now); err != nil {
			return c, err
		}
	}
	if t.RequiredRole == string(models.RoleCurator) {
		c.Specialists = c.Curators
	} else if c.Specialists, err = byRole(t.RequiredRole, true); err != nil {
		return c, err
	}
	if c.OrgAdmins, err = byRole(string(models.RoleOrgAdmin), true); err != nil {
		return c, err
	}
	// Системный администратор арендатора не привязан: подходит и «без арендатора».
	if c.SuperAdmins, err = byRole(string(models.RoleSuperAdmin), false); err != nil {
		return c, err
	}
	return c, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// NewTask — постановка задачи.
type NewTask struct {
	TenantID    string
	PartnerID   string
	Kind        string
	Title       string
	SubjectType string
	SubjectID   string
	Role        models.Role
	Permission  rbac.Permission
	CreatedBy   string
}

func (n NewTask) validate() error {
	if n.TenantID == "" || strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.SubjectID) == "" {
		return fmt.Errorf("%w: укажите арендатора, заголовок и предмет", ErrInvalid)
	}
	if _, known := rbac.Roles[n.Role]; !known {
		return fmt.Errorf("%w: неизвестная роль %q", ErrInvalid, n.Role)
	}
	if !rbac.Known(n.Permission) {
		return fmt.Errorf("%w: неизвестное полномочие %q", ErrInvalid, n.Permission)
	}
	return nil
}

func insertEvent(ctx context.Context, db queryer, taskID, action, from, to string, route Route, reason, actor string) error {
	var actorArg, fromArg, toArg interface{}
	if actor != "" {
		actorArg = actor
	}
	if from != "" {
		fromArg = from
	}
	if to != "" {
		toArg = to
	}
	_, err := db.ExecContext(ctx, `INSERT INTO workflow_task_events(task_id,action,from_assignee,to_assignee,route,reason,actor_id,system_action)
		VALUES($1::uuid,$2,$3::uuid,$4::uuid,$5,NULLIF($6,''),$7::uuid,$8)`,
		taskID, action, fromArg, toArg, string(route), reason, actorArg, actor == "")
	return err
}

// Dispatch ставит задачу и назначает исполнителя по цепочке ADR-22.
// Повторная постановка той же задачи (вид, предмет) возвращает уже открытую и
// ничего не меняет: created=false.
func Dispatch(ctx context.Context, tx *sql.Tx, in NewTask, now time.Time) (task Task, created bool, err error) {
	if err := in.validate(); err != nil {
		return Task{}, false, err
	}
	if in.PartnerID != "" {
		var belongs bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text=$1 AND it_company_id::text=$2)`, in.PartnerID, in.TenantID).Scan(&belongs); err != nil {
			return Task{}, false, err
		}
		if !belongs {
			return Task{}, false, ErrPartnerTenant
		}
	}
	probe := Task{ITCompanyID: in.TenantID, PartnerID: in.PartnerID, RequiredRole: string(in.Role), RequiredPermission: string(in.Permission)}
	c, err := candidates(ctx, tx, probe, now)
	if err != nil {
		return Task{}, false, err
	}
	decision := Resolve(probe.requirement(), c)

	var assignee, partner, creator interface{}
	if decision.Assignee != "" {
		assignee = decision.Assignee
	}
	if in.PartnerID != "" {
		partner = in.PartnerID
	}
	if in.CreatedBy != "" {
		creator = in.CreatedBy
	}
	row := tx.QueryRowContext(ctx, `INSERT INTO workflow_tasks(it_company_id,partner_id,kind,title,subject_type,subject_id,required_role,
			required_permission,assignee_id,route,unassigned_escalated,fallback_reason,created_by)
		VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9::uuid,$10,$11,NULLIF($12,''),$13::uuid)
		ON CONFLICT (kind,subject_type,subject_id) WHERE status='open' DO NOTHING
		RETURNING `+taskColumns, in.TenantID, partner, in.Kind, strings.TrimSpace(in.Title), in.SubjectType, strings.TrimSpace(in.SubjectID),
		string(in.Role), string(in.Permission), assignee, string(decision.Route), decision.Escalated, decision.Reason, creator)
	task, err = scanTask(row)
	if err == nil {
		return task, true, insertEvent(ctx, tx, task.ID, "created", "", decision.Assignee, decision.Route, decision.Reason, in.CreatedBy)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23514" {
			return Task{}, false, fmt.Errorf("%w: %s", ErrInvalid, pgErr.Message)
		}
		return Task{}, false, err
	}
	existing, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM workflow_tasks
		WHERE kind=$1 AND subject_type=$2 AND subject_id=$3 AND status='open'`, in.Kind, in.SubjectType, strings.TrimSpace(in.SubjectID)))
	return existing, false, err
}

// Get читает задачу.
func Get(ctx context.Context, db queryer, id string) (Task, error) {
	t, err := scanTask(db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM workflow_tasks WHERE id::text=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return t, err
}

// History — история задачи в порядке событий.
func History(ctx context.Context, db queryer, id string) ([]Event, error) {
	rows, err := db.QueryContext(ctx, `SELECT action,COALESCE(from_assignee::text,''),COALESCE(to_assignee::text,''),route,
			COALESCE(reason,''),COALESCE(actor_id::text,''),system_action,occurred_at::text
		FROM workflow_task_events WHERE task_id::text=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Action, &e.FromAssignee, &e.ToAssignee, &e.Route, &e.Reason, &e.ActorID, &e.System, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Filter ограничивает список задач.
type Filter struct {
	TenantID   string
	AssigneeID string
	Status     string
	Escalated  bool
}

// List возвращает задачи, новые первыми.
func List(ctx context.Context, db queryer, f Filter) ([]Task, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+taskColumns+` FROM workflow_tasks
		WHERE ($1='' OR it_company_id::text=$1) AND ($2='' OR assignee_id::text=$2) AND ($3='' OR status=$3)
		  AND (NOT $4 OR unassigned_escalated)
		ORDER BY created_at DESC,id LIMIT 500`, f.TenantID, f.AssigneeID, f.Status, f.Escalated)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Reassign заново проходит цепочку для открытой задачи. Меняет исполнителя
// только если решение отличается: пока исполнитель остаётся допустимым на том
// же шаге, он сохраняется, чтобы плановый пересчёт не гонял задачу между
// одинаково подходящими людьми. Ручная попытка без изменений тоже остаётся в
// истории — по ADR-22 записываются и попытки переназначения.
func Reassign(ctx context.Context, tx *sql.Tx, id, actor, reason string, now time.Time) (Task, bool, error) {
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM workflow_tasks WHERE id::text=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, ErrNotFound
	}
	if err != nil {
		return Task{}, false, err
	}
	if task.Status != "open" {
		return task, false, ErrNotOpen
	}
	c, err := candidates(ctx, tx, task, now)
	if err != nil {
		return task, false, err
	}
	decision := Resolve(task.requirement(), c)

	if decision.Route == task.Route && decision.Assignee != "" && stillValid(task, decision.Route, c) {
		if actor != "" {
			return task, false, insertEvent(ctx, tx, task.ID, "reassign_attempt", task.AssigneeID, task.AssigneeID, task.Route,
				strings.TrimSpace(reason)+" (исполнитель остаётся допустимым)", actor)
		}
		return task, false, nil
	}
	if decision.Route == task.Route && decision.Assignee == task.AssigneeID {
		if actor != "" {
			return task, false, insertEvent(ctx, tx, task.ID, "reassign_attempt", task.AssigneeID, task.AssigneeID, task.Route, strings.TrimSpace(reason), actor)
		}
		return task, false, nil
	}
	var assignee interface{}
	if decision.Assignee != "" {
		assignee = decision.Assignee
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_tasks SET assignee_id=$2::uuid,route=$3,unassigned_escalated=$4,
			fallback_reason=NULLIF($5,''),updated_at=now() WHERE id::text=$1`,
		id, assignee, string(decision.Route), decision.Escalated, decision.Reason); err != nil {
		return task, false, err
	}
	note := decision.Reason
	if r := strings.TrimSpace(reason); r != "" {
		note = r + "; " + note
	}
	if err := insertEvent(ctx, tx, id, "reassigned", task.AssigneeID, decision.Assignee, decision.Route, strings.Trim(note, "; "), actor); err != nil {
		return task, false, err
	}
	updated, err := Get(ctx, tx, id)
	return updated, true, err
}

// stillValid — действующий исполнитель по-прежнему среди кандидатов своего шага.
func stillValid(t Task, route Route, c Candidates) bool {
	var pool []string
	switch route {
	case RouteSpecialist:
		pool = c.Specialists
	case RouteCurator:
		pool = c.Curators
	case RouteOrgAdmin:
		pool = c.OrgAdmins
	case RouteSuperAdmin:
		pool = c.SuperAdmins
	}
	for _, id := range pool {
		if id == t.AssigneeID {
			return true
		}
	}
	return false
}

// Complete закрывает задачу от имени исполнителя. Полномочия проверяет
// вызывающий по CanExecute; здесь — состояние и атомарность.
func Complete(ctx context.Context, tx *sql.Tx, id, actor string) (Task, error) {
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM workflow_tasks WHERE id::text=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if task.Status != "open" {
		return task, ErrNotOpen
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_tasks SET status='done',completed_by=$2::uuid,completed_at=now(),updated_at=now() WHERE id::text=$1`, id, actor); err != nil {
		return task, err
	}
	if err := insertEvent(ctx, tx, id, "completed", task.AssigneeID, task.AssigneeID, task.Route, "", actor); err != nil {
		return task, err
	}
	return Get(ctx, tx, id)
}

// RedispatchOpen пересчитывает открытые задачи, которые идут не по основной
// дороге или чей исполнитель больше не действует: специалист появился, куратор
// перестал быть закреплённым, пользователь отключён. Возвращает число
// переназначенных. tenantIDs ограничивает проход этими арендаторами; в работе не
// задаётся. Каждая задача — в своей транзакции; повторный запуск ничего
// не меняет.
func RedispatchOpen(ctx context.Context, db *sql.DB, now time.Time, tenantIDs ...string) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT w.id::text FROM workflow_tasks w
		LEFT JOIN users a ON a.id=w.assignee_id
		WHERE w.status='open' AND (w.route<>'specialist' OR a.id IS NULL OR NOT a.is_active)
		  AND (cardinality($1::uuid[])=0 OR w.it_company_id=ANY($1::uuid[]))
		ORDER BY w.created_at,w.id LIMIT 500`, pq.Array(tenantIDs))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	changed := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return changed, ctx.Err()
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return changed, err
		}
		before, _ := Get(ctx, tx, id)
		after, moved, err := Reassign(ctx, tx, id, "", "плановый пересчёт", now)
		if err != nil && !errors.Is(err, ErrNotOpen) {
			tx.Rollback()
			return changed, fmt.Errorf("задача %s: %w", id, err)
		}
		if moved {
			// Действие системы, а не человека: в журнале аудита актор — система.
			if err := platformaudit.Write(ctx, tx, platformaudit.Event{Actor: platformaudit.UserActor(""),
				Action: "reassign", Entity: platformaudit.Entity{Type: "workflow_task", ID: id},
				Before: before, After: after, Comment: after.FallbackReason}); err != nil {
				tx.Rollback()
				return changed, fmt.Errorf("задача %s: аудит: %w", id, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return changed, err
		}
		if moved {
			changed++
		}
	}
	return changed, nil
}

// RunRedispatch запускает RedispatchOpen по расписанию до закрытия stop.
func RunRedispatch(db *sql.DB, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_, _ = RedispatchOpen(ctx, db, time.Now())
		cancel()
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}
