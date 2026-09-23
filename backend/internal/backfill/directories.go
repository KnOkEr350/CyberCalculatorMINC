package backfill

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Правила пометок справочников (DATA-13).
const (
	RuleAgreementNeedsReview   = "agreement.legacy_needs_review"
	RuleAgreementTechnicalNo   = "agreement.technical_number"
	RuleAgreementOpenEnded     = "agreement.open_ended_period"
	RulePartnerNoDirectory     = "partner.no_directory_link"
	RulePartnerNoCompany       = "partner.no_it_company"
	RulePartnerDuplicateName   = "partner.duplicate_name"
	RuleITCompanyNoRequisites  = "it_company.no_requisites"
	RuleITCompanyNoAccredation = "it_company.no_accreditation_number"
)

// DirectoriesReport — итог прогона по справочникам.
type DirectoriesReport struct {
	// Detected — сколько находок обнаружено в этом прогоне по каждому правилу.
	Detected map[string]int
	// Resolved — сколько ранее открытых находок закрыто, потому что причина исправлена.
	Resolved int
	Total    int
}

func (r DirectoriesReport) String() string {
	rules := make([]string, 0, len(r.Detected))
	for rule := range r.Detected {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	text := fmt.Sprintf("открытых находок %d, закрыто исправленных %d", r.Total, r.Resolved)
	for _, rule := range rules {
		text += fmt.Sprintf("; %s: %d", rule, r.Detected[rule])
	}
	return text
}

// directoryRule — запрос, возвращающий (entity_id, it_company_id, detail) для
// строк, у которых правило нарушено.
type directoryRule struct {
	entity, code, query string
}

var directoryRules = []directoryRule{
	{"agreement", RuleAgreementNeedsReview, `SELECT id, it_company_id, 'Соглашение перенесено из прежней модели: реквизиты и статус необходимо проверить'
		FROM agreements WHERE status = 'needs_review'`},
	{"agreement", RuleAgreementTechnicalNo, `SELECT id, it_company_id, 'Номер соглашения технический (' || number || '): укажите реквизиты договора'
		FROM agreements WHERE number LIKE 'LEGACY-%'`},
	{"agreement", RuleAgreementOpenEnded, `SELECT id, it_company_id, 'Срок действия не ограничен (9999-12-31): укажите дату окончания'
		FROM agreements WHERE valid_until = DATE '9999-12-31'`},
	{"partner", RulePartnerNoDirectory, `SELECT id, it_company_id, 'Партнёр «' || name || '» не связан с проверенным справочником образовательных организаций'
		FROM partners WHERE directory_id IS NULL`},
	{"partner", RulePartnerNoCompany, `SELECT id, NULL::uuid, 'Партнёр «' || name || '» не привязан к ИТ-компании'
		FROM partners WHERE it_company_id IS NULL`},
	// Два партнёра одной ИТ-компании с одним названием без учёта регистра и
	// лишних пробелов: какой из них настоящий — решает человек.
	{"partner", RulePartnerDuplicateName, `SELECT p.id, p.it_company_id, 'Название «' || p.name || '» повторяется у другого партнёра этой ИТ-компании'
		FROM partners p WHERE p.it_company_id IS NOT NULL AND EXISTS (
			SELECT 1 FROM partners q WHERE q.id <> p.id AND q.it_company_id = p.it_company_id
			  AND lower(regexp_replace(btrim(q.name), '\s+', ' ', 'g')) = lower(regexp_replace(btrim(p.name), '\s+', ' ', 'g')))`},
	{"it_company", RuleITCompanyNoRequisites, `SELECT id, id, 'У ИТ-компании «' || name || '» не заполнены ' || concat_ws(', ',
			CASE WHEN COALESCE(btrim(inn), '') = '' THEN 'ИНН' END, CASE WHEN COALESCE(btrim(ogrn), '') = '' THEN 'ОГРН' END)
		FROM accredited_it_companies WHERE COALESCE(btrim(inn), '') = '' OR COALESCE(btrim(ogrn), '') = ''`},
	{"it_company", RuleITCompanyNoAccredation, `SELECT id, id, 'У ИТ-компании «' || name || '» не указан номер аккредитации'
		FROM accredited_it_companies WHERE COALESCE(btrim(accreditation_number), '') = ''`},
}

// Directories находит в справочниках строки, перенесённые с пробелами или
// неоднозначные, и ведёт журнал находок. Данные справочников не меняются.
// Прогон идемпотентен: повторный запуск ничего не добавляет, а находки,
// причина которых исправлена, закрываются.
func Directories(ctx context.Context, db *sql.DB) (DirectoriesReport, error) {
	report := DirectoriesReport{Detected: map[string]int{}}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	// Один прогон за раз: параллельный запуск дождётся, а не пересечётся.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('backfill:directories', 0))`); err != nil {
		return report, err
	}
	for _, rule := range directoryRules {
		result, err := tx.ExecContext(ctx, `WITH found(id, it_company_id, detail) AS (`+rule.query+`)
			INSERT INTO directory_backfill_findings(entity_type, entity_id, it_company_id, rule_code, detail)
			SELECT $1, f.id, f.it_company_id, $2, f.detail FROM found f
			ON CONFLICT (entity_type, entity_id, rule_code) DO UPDATE
			  SET detail = EXCLUDED.detail, it_company_id = EXCLUDED.it_company_id, resolved_at = NULL`, rule.entity, rule.code)
		if err != nil {
			return report, fmt.Errorf("правило %s: %w", rule.code, err)
		}
		n, _ := result.RowsAffected()
		report.Detected[rule.code] = int(n)
		// Закрываются находки, которых в этом прогоне больше нет.
		closed, err := tx.ExecContext(ctx, `WITH found(id, it_company_id, detail) AS (`+rule.query+`)
			UPDATE directory_backfill_findings d SET resolved_at = now()
			WHERE d.entity_type = $1 AND d.rule_code = $2 AND d.resolved_at IS NULL
			  AND NOT EXISTS (SELECT 1 FROM found f WHERE f.id = d.entity_id)`, rule.entity, rule.code)
		if err != nil {
			return report, fmt.Errorf("правило %s: закрытие: %w", rule.code, err)
		}
		c, _ := closed.RowsAffected()
		report.Resolved += int(c)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM directory_backfill_findings WHERE resolved_at IS NULL`).Scan(&report.Total); err != nil {
		return report, err
	}
	return report, tx.Commit()
}
