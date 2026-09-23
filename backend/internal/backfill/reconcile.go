package backfill

import (
	"context"
	"database/sql"
	"fmt"
)

// Gate — один барьер сверки перед отключением старого чтения (INTG-04).
type Gate struct {
	Name   string
	Passed bool
	Detail string
}

// ReconcileReport — результат сверки: перейти на новую модель можно, когда
// пройдены все барьеры целостности, а открытых находок переноса не осталось.
type ReconcileReport struct {
	Gates []Gate
	// OpenFindings — не разобранные находки переноса записей и справочников.
	OpenFindings int
}

// Consistent — все барьеры целостности пройдены.
func (r ReconcileReport) Consistent() bool {
	for _, gate := range r.Gates {
		if !gate.Passed {
			return false
		}
	}
	return true
}

// ReadyForCutover — целостность подтверждена и разбирать больше нечего.
func (r ReconcileReport) ReadyForCutover() bool { return r.Consistent() && r.OpenFindings == 0 }

func (r ReconcileReport) String() string {
	text := fmt.Sprintf("барьеров %d, целостность %v, открытых находок %d, готовность к переходу %v",
		len(r.Gates), r.Consistent(), r.OpenFindings, r.ReadyForCutover())
	for _, gate := range r.Gates {
		if !gate.Passed {
			text += fmt.Sprintf("; НЕ ПРОЙДЕН %s: %s", gate.Name, gate.Detail)
		}
	}
	return text
}

// gates — проверки «должно быть 0»: каждая возвращает число нарушающих строк.
var gates = []struct{ name, description, query string }{
	{"ood_rpd.typed_matches_payload", "типизированные поля ООП/РПД совпадают с payload",
		`SELECT count(*) FROM entries WHERE category_code='ood_rpd'
		   AND (oop_doc_type IS DISTINCT FROM CASE WHEN lower(btrim(COALESCE(payload->>'doc_type','')))  IN ('rpd','oop') THEN lower(btrim(payload->>'doc_type')) END
		     OR oop_activity IS DISTINCT FROM CASE WHEN lower(btrim(COALESCE(payload->>'activity_type','')))  IN ('development','update','expertise') THEN lower(btrim(payload->>'activity_type')) END)`},
	{"ood_rpd.incomplete_are_reported", "каждая неполная запись ООП/РПД есть в журнале находок",
		`SELECT count(*) FROM entries e WHERE e.category_code='ood_rpd' AND e.oop_incomplete
		   AND NOT EXISTS (SELECT 1 FROM legacy_backfill_findings f WHERE f.entry_id=e.id AND f.rule_code='oop.model_incomplete')`},
	{"top_it.only_higher_education", "запись Вида 4 не по ВО помечена как перенесённая и не идёт в зачёт",
		`SELECT count(*) FROM entries WHERE category_code='top_it' AND audience<>'vuz' AND NOT top_legacy_non_vo`},
	{"top_it.non_vo_are_reported", "каждая запись Вида 4 не по ВО есть в журнале находок",
		`SELECT count(*) FROM entries e WHERE e.category_code='top_it' AND e.top_legacy_non_vo
		   AND NOT EXISTS (SELECT 1 FROM legacy_backfill_findings f WHERE f.entry_id=e.id AND f.rule_code='top.non_vo_audience')`},
	{"top_it.non_vo_not_eligible", "запись Вида 4 не по ВО не допущена к зачёту",
		`SELECT count(*) FROM entry_eligibility el JOIN entries e ON e.id=el.id WHERE e.top_legacy_non_vo AND el.eligible`},
	{"school.incomplete_are_reported", "каждая неполная школьная запись есть в журнале находок",
		`SELECT count(*) FROM entries e WHERE e.school_incomplete
		   AND NOT EXISTS (SELECT 1 FROM legacy_backfill_findings f WHERE f.entry_id=e.id AND f.rule_code='school.program_missing')`},
	{"entries.tenant_matches_agreement", "арендатор записи совпадает с арендатором соглашения",
		`SELECT count(*) FROM entries e JOIN agreements a ON a.id=e.agreement_id WHERE e.it_company_id IS DISTINCT FROM a.it_company_id`},
	{"agreements.have_revision", "у каждого соглашения есть хотя бы одна редакция истории",
		`SELECT count(*) FROM agreements a WHERE NOT EXISTS (SELECT 1 FROM agreement_revisions r WHERE r.agreement_id=a.id)`},
	{"curators.cache_matches_assignments", "закрепление куратора в профиле совпадает с действующим закреплением",
		`SELECT count(*) FROM users u WHERE u.role='curator' AND u.entity_type='organization'
		   AND u.partner_id IS DISTINCT FROM current_partner_of(u.id)`},
}

// Reconcile проверяет целостность перенесённых данных и сообщает, остались ли
// неразобранные находки. Ничего не меняет.
func Reconcile(ctx context.Context, db *sql.DB) (ReconcileReport, error) {
	var report ReconcileReport
	for _, gate := range gates {
		var violations int
		if err := db.QueryRowContext(ctx, gate.query).Scan(&violations); err != nil {
			return report, fmt.Errorf("барьер %s: %w", gate.name, err)
		}
		detail := gate.description
		if violations > 0 {
			detail = fmt.Sprintf("%s: нарушений %d", gate.description, violations)
		}
		report.Gates = append(report.Gates, Gate{Name: gate.name, Passed: violations == 0, Detail: detail})
	}
	var entries, directories int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM legacy_backfill_findings WHERE resolved_at IS NULL`).Scan(&entries); err != nil {
		return report, err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings WHERE resolved_at IS NULL`).Scan(&directories); err != nil {
		return report, err
	}
	report.OpenFindings = entries + directories
	return report, nil
}
