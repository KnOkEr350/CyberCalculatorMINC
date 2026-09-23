// Package backfill дозаполняет старые записи после смены модели данных
// (TCH-08). Правило одно: связывается только то, что определяется однозначно
// и ничего не ломает; остальное остаётся как есть и помечается на ручное
// уточнение. Недостающие сведения не выдумываются.
package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Правила пометок. Код виден оператору и попадает в отчёт.
const (
	RuleTeacherNameMissing  = "teachers.name_missing"
	RuleTeacherNoMatch      = "teachers.no_staff_match"
	RuleTeacherAmbiguous    = "teachers.ambiguous_staff"
	RuleTeacherReportLocked = "teachers.report_locked"
)

// TeachersReport — итог прогона. Каждая запись без сотрудника попадает ровно в
// один счётчик, поэтому отчёт сходится с числом обработанных записей.
type TeachersReport struct {
	Linked        int // сотрудник найден однозначно и связан
	NameMissing   int // в записи нет ФИО и сотрудника — нечем искать
	NoMatch       int // сотрудника с таким ФИО нет в справочнике ИТ-компании
	Ambiguous     int // подходит несколько сотрудников
	ReportLocked  int // сотрудник найден, но связывание сбросило бы утверждённый отчёт
	Resolved      int // ранее помеченные записи, которые с тех пор исправлены
	AlreadyLinked int // записи, где сотрудник уже указан (в работе не участвуют)
}

// Manual — число записей, оставшихся на ручное уточнение.
func (r TeachersReport) Manual() int {
	return r.NameMissing + r.NoMatch + r.Ambiguous + r.ReportLocked
}

func (r TeachersReport) String() string {
	return fmt.Sprintf("связано %d; на ручное уточнение %d (нет ФИО %d, нет в справочнике %d, неоднозначно %d, утверждённый отчёт %d); исправлено с прошлого прогона %d; уже связано %d",
		r.Linked, r.Manual(), r.NameMissing, r.NoMatch, r.Ambiguous, r.ReportLocked, r.Resolved, r.AlreadyLinked)
}

type candidate struct {
	id, company, agreement, period string
	year                           int
	name, payloadStaff             string
}

// Teachers связывает записи о педагогической нагрузке со справочником
// сотрудников. Прогон идемпотентен: повторный запуск ничего не меняет, а
// пометки о записях, исправленных вручную, снимаются.
func Teachers(ctx context.Context, db *sql.DB) (TeachersReport, error) {
	var report TeachersReport
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM entries WHERE category_code='teachers' AND staff_member_id IS NOT NULL`).
		Scan(&report.AlreadyLinked); err != nil {
		return report, err
	}

	// Записи, которые с прошлого прогона получили сотрудника (вручную через
	// обычный интерфейс), больше не требуют внимания.
	result, err := db.ExecContext(ctx, `UPDATE legacy_backfill_findings f
		SET resolved_at=now(), resolved_reason='сотрудник указан'
		FROM entries e
		WHERE e.id=f.entry_id AND f.category_code='teachers' AND f.rule_code LIKE 'teachers.%'
		  AND f.resolved_at IS NULL AND e.staff_member_id IS NOT NULL`)
	if err != nil {
		return report, err
	}
	if n, rowsErr := result.RowsAffected(); rowsErr == nil {
		report.Resolved = int(n)
	}

	rows, err := db.QueryContext(ctx, `SELECT e.id::text,COALESCE(e.it_company_id::text,''),
			COALESCE(e.agreement_id::text,''),e.period_type,e.report_year,
			COALESCE(e.payload->>'teacher_full_name',''),COALESCE(e.payload->>'staff_member_id','')
		FROM entries e WHERE e.category_code='teachers' AND e.staff_member_id IS NULL
		ORDER BY e.created_at,e.id`)
	if err != nil {
		return report, err
	}
	var pending []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.company, &c.agreement, &c.period, &c.year, &c.name, &c.payloadStaff); err != nil {
			rows.Close()
			return report, err
		}
		pending = append(pending, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	for _, item := range pending {
		if ctx.Err() != nil {
			return report, ctx.Err()
		}
		outcome, err := backfillOne(ctx, db, item)
		if err != nil {
			return report, fmt.Errorf("запись %s: %w", item.id, err)
		}
		switch outcome {
		case outcomeLinked:
			report.Linked++
		case RuleTeacherNameMissing:
			report.NameMissing++
		case RuleTeacherNoMatch:
			report.NoMatch++
		case RuleTeacherAmbiguous:
			report.Ambiguous++
		case RuleTeacherReportLocked:
			report.ReportLocked++
		}
	}
	return report, nil
}

const outcomeLinked = "linked"

// backfillOne обрабатывает одну запись в собственной транзакции: сбой на одной
// записи не откатывает связывание остальных.
func backfillOne(ctx context.Context, db *sql.DB, item candidate) (string, error) {
	if item.company == "" {
		return flag(ctx, db, item, RuleTeacherNoMatch, "у записи не указана ИТ-компания", "")
	}
	name := normalizeName(item.name)

	// Сотрудник, названный в самой записи, надёжнее совпадения по ФИО.
	if id := strings.TrimSpace(item.payloadStaff); id != "" {
		var found string
		err := db.QueryRowContext(ctx,
			`SELECT id::text FROM staff_members WHERE id::text=$1 AND it_company_id::text=$2`, id, item.company).Scan(&found)
		if err == nil {
			return link(ctx, db, item, found)
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}
	if name == "" {
		return flag(ctx, db, item, RuleTeacherNameMissing, "в записи нет ФИО преподавателя и ссылки на сотрудника", "")
	}

	rows, err := db.QueryContext(ctx, `SELECT id::text FROM staff_members
		WHERE it_company_id::text=$1 AND lower(regexp_replace(btrim(fio),'\s+',' ','g'))=lower($2)`, item.company, name)
	if err != nil {
		return "", err
	}
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", err
		}
		matches = append(matches, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return flag(ctx, db, item, RuleTeacherNoMatch,
			fmt.Sprintf("сотрудник %q не найден в справочнике ИТ-компании", item.name), "")
	case 1:
		return link(ctx, db, item, matches[0])
	default:
		return flag(ctx, db, item, RuleTeacherAmbiguous,
			fmt.Sprintf("ФИО %q подходит нескольким сотрудникам: %s", item.name, strings.Join(matches, ", ")), "")
	}
}

// link связывает запись с сотрудником, если это не сбросит утверждённый отчёт:
// любое изменение записи возвращает отчёт соглашения в черновик, и молча
// отменять чужое утверждение дозаполнение не вправе.
func link(ctx context.Context, db *sql.DB, item candidate, staffID string) (string, error) {
	var status sql.NullString
	if item.agreement != "" {
		err := db.QueryRowContext(ctx, `SELECT status FROM agreement_reports
			WHERE agreement_id::text=$1 AND report_year=$2 AND period_type=$3`,
			item.agreement, item.year, item.period).Scan(&status)
		if err != nil && err != sql.ErrNoRows {
			return "", err
		}
	}
	if status.Valid && status.String != "draft" {
		return flag(ctx, db, item, RuleTeacherReportLocked,
			fmt.Sprintf("отчёт соглашения в статусе %q: связывание вернуло бы его в черновик", status.String), staffID)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE entries SET staff_member_id=$1::uuid WHERE id::text=$2 AND staff_member_id IS NULL`,
		staffID, item.id); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE legacy_backfill_findings SET resolved_at=now(),resolved_reason='связано с сотрудником'
		WHERE entry_id::text=$1 AND resolved_at IS NULL`, item.id); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return outcomeLinked, nil
}

// flag фиксирует пометку. Повтор обновляет описание, а не плодит записи.
func flag(ctx context.Context, db *sql.DB, item candidate, rule, detail, suggested string) (string, error) {
	_, err := db.ExecContext(ctx, `INSERT INTO legacy_backfill_findings(entry_id,category_code,rule_code,detail,suggested_value)
		VALUES($1::uuid,'teachers',$2,$3,NULLIF($4,''))
		ON CONFLICT (entry_id,rule_code) DO UPDATE SET detail=EXCLUDED.detail,suggested_value=EXCLUDED.suggested_value,
			resolved_at=NULL,resolved_reason=NULL`, item.id, rule, detail, suggested)
	if err != nil {
		return "", err
	}
	// Другие правила по той же записи снимаются: причина у неё теперь одна.
	if _, err := db.ExecContext(ctx, `UPDATE legacy_backfill_findings SET resolved_at=now(),resolved_reason='причина изменилась'
		WHERE entry_id::text=$1 AND rule_code<>$2 AND resolved_at IS NULL`, item.id, rule); err != nil {
		return "", err
	}
	return rule, nil
}

// normalizeName схлопывает пробелы. Регистр приводит сама БД с обеих сторон
// сравнения: так результат не зависит от локали сервера и совпадает с тем, как
// справочник сравнивает ФИО в других местах.
func normalizeName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}
