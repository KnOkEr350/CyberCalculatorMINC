// Package aggregates держит кэш агрегатов вклада мероприятий (RISK-08).
//
// Кэш строится из общей проекции (activityprojection.Reader) и ничего не
// добавляет к источнику: каждая сумма — та же, что даёт проекция. Пересборка
// идемпотентна, атомарна по (арендатор, год) и защищена от параллельного
// запуска. Verify пересчитывает срез с нуля и сообщает любое расхождение с
// кэшем — на этом держится проверка «суммы совпадают с источником».
package aggregates

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"cybercalc/internal/money"
	"cybercalc/internal/platform/activityprojection"
)

// Row — одна строка кэша.
type Row struct {
	PartnerID, AgreementID, CategoryCode, Audience string
	PlanEntries, FactEntries                       int
	PlanUnits, FactUnits                           float64
	PlanAmount, FactAmount                         money.Amount
	CalculatedFact, ConfirmedFact, CountedFact     money.Amount
	EligiblePlan, EligibleFact                     money.Amount
	IncompleteEntries                              int
}

func (r Row) key() string {
	return r.PartnerID + "\x00" + r.AgreementID + "\x00" + r.CategoryCode + "\x00" + r.Audience
}

// fromProjection считает срез тем же кодом, что и остальные потребители проекции.
func fromProjection(ctx context.Context, reader activityprojection.Reader, tenantID string, year int) ([]Row, int, error) {
	items, err := reader.List(ctx, activityprojection.Filter{TenantID: tenantID, ReportYear: year})
	if err != nil {
		return nil, 0, err
	}
	// Ключ включает вид и аудиторию: проекция группирует по всем четырём измерениям.
	groups, err := activityprojection.AggregateContributions(items,
		activityprojection.Grouping{Partner: true, Agreement: true, Category: true, Audience: true})
	if err != nil {
		return nil, 0, err
	}
	rows := make([]Row, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, Row{
			PartnerID: g.PartnerID, AgreementID: g.AgreementID, CategoryCode: g.CategoryCode, Audience: g.Audience,
			PlanEntries: g.PlanEntries, FactEntries: g.FactEntries, PlanUnits: g.PlanUnits, FactUnits: g.FactUnits,
			PlanAmount: g.PlanAmount, FactAmount: g.FactAmount, CalculatedFact: g.CalculatedFactAmount,
			ConfirmedFact: g.ConfirmedFactAmount, CountedFact: g.CountedFactAmount,
			EligiblePlan: g.EligiblePlanAmount, EligibleFact: g.EligibleFactAmount, IncompleteEntries: g.IncompleteEntries,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].key() < rows[j].key() })
	return rows, len(items), nil
}

// Rebuild пересобирает срез арендатора за год. Повторный вызов на тех же данных
// даёт то же содержимое. Возвращает число строк.
func Rebuild(ctx context.Context, db *sql.DB, reader activityprojection.Reader, tenantID string, year int) (int, error) {
	if tenantID == "" {
		return 0, errors.New("aggregates: не указан арендатор")
	}
	rows, contributions, err := fromProjection(ctx, reader, tenantID, year)
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Один пересбор на пару за раз: параллельный запуск дождётся, а не вмешается.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('aggregates:'||$1||':'||$2::text,0))`, tenantID, year); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM activity_aggregates WHERE tenant_id=$1::uuid AND report_year=$2`, tenantID, year); err != nil {
		return 0, err
	}
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO activity_aggregates(tenant_id,report_year,partner_id,agreement_id,category_code,audience,
				plan_entries,fact_entries,plan_units,fact_units,plan_amount_rub,fact_amount_rub,calculated_fact_rub,confirmed_fact_rub,
				counted_fact_rub,eligible_plan_rub,eligible_fact_rub,incomplete_entries)
			VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::numeric,$12::numeric,$13::numeric,$14::numeric,$15::numeric,$16::numeric,$17::numeric,$18)`,
			tenantID, year, r.PartnerID, r.AgreementID, r.CategoryCode, r.Audience, r.PlanEntries, r.FactEntries, r.PlanUnits, r.FactUnits,
			r.PlanAmount.String(), r.FactAmount.String(), r.CalculatedFact.String(), r.ConfirmedFact.String(), r.CountedFact.String(),
			r.EligiblePlan.String(), r.EligibleFact.String(), r.IncompleteEntries); err != nil {
			return 0, fmt.Errorf("aggregates: строка %s/%s: %w", r.CategoryCode, r.Audience, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO activity_aggregate_builds(tenant_id,report_year,contributions,built_at)
		VALUES($1::uuid,$2,$3,now()) ON CONFLICT (tenant_id,report_year) DO UPDATE SET contributions=EXCLUDED.contributions,built_at=now()`,
		tenantID, year, contributions); err != nil {
		return 0, err
	}
	return len(rows), tx.Commit()
}

// Read читает кэш арендатора за год в стабильном порядке.
func Read(ctx context.Context, db *sql.DB, tenantID string, year int) ([]Row, error) {
	rows, err := db.QueryContext(ctx, `SELECT partner_id,agreement_id,category_code,audience,plan_entries,fact_entries,plan_units,fact_units,
			plan_amount_rub::text,fact_amount_rub::text,calculated_fact_rub::text,confirmed_fact_rub::text,counted_fact_rub::text,
			eligible_plan_rub::text,eligible_fact_rub::text,incomplete_entries
		FROM activity_aggregates WHERE tenant_id=$1::uuid AND report_year=$2
		ORDER BY partner_id,agreement_id,category_code,audience`, tenantID, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		var r Row
		var amounts [7]string
		if err := rows.Scan(&r.PartnerID, &r.AgreementID, &r.CategoryCode, &r.Audience, &r.PlanEntries, &r.FactEntries, &r.PlanUnits, &r.FactUnits,
			&amounts[0], &amounts[1], &amounts[2], &amounts[3], &amounts[4], &amounts[5], &amounts[6], &r.IncompleteEntries); err != nil {
			return nil, err
		}
		targets := []*money.Amount{&r.PlanAmount, &r.FactAmount, &r.CalculatedFact, &r.ConfirmedFact, &r.CountedFact, &r.EligiblePlan, &r.EligibleFact}
		for i, text := range amounts {
			amount, err := money.Parse(text)
			if err != nil {
				return nil, fmt.Errorf("aggregates: сумма %q: %w", text, err)
			}
			*targets[i] = amount
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Mismatch — расхождение между кэшем и заново посчитанным срезом.
type Mismatch struct {
	Key    string
	Detail string
}

// Verify пересчитывает срез из источника и сравнивает с кэшем построчно.
// Пустой результат — кэш точен. Отсутствующая или лишняя строка — расхождение.
func Verify(ctx context.Context, db *sql.DB, reader activityprojection.Reader, tenantID string, year int) ([]Mismatch, error) {
	fresh, _, err := fromProjection(ctx, reader, tenantID, year)
	if err != nil {
		return nil, err
	}
	cached, err := Read(ctx, db, tenantID, year)
	if err != nil {
		return nil, err
	}
	byKey := map[string]Row{}
	for _, r := range cached {
		byKey[r.key()] = r
	}
	var out []Mismatch
	for _, want := range fresh {
		got, ok := byKey[want.key()]
		if !ok {
			out = append(out, Mismatch{Key: want.key(), Detail: "строки нет в кэше"})
			continue
		}
		delete(byKey, want.key())
		if got != want {
			out = append(out, Mismatch{Key: want.key(), Detail: fmt.Sprintf("кэш %+v, источник %+v", got, want)})
		}
	}
	for key := range byKey {
		out = append(out, Mismatch{Key: key, Detail: "лишняя строка в кэше"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// RebuildAll пересобирает срезы всех арендаторов и лет, у которых есть
// мероприятия. Ошибка одного среза не мешает остальным: возвращается первая.
func RebuildAll(ctx context.Context, db *sql.DB, reader activityprojection.Reader) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT it_company_id::text,report_year FROM entries WHERE it_company_id IS NOT NULL ORDER BY 1,2`)
	if err != nil {
		return 0, err
	}
	type pair struct {
		tenant string
		year   int
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.tenant, &p.year); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	built := 0
	var first error
	for _, p := range pairs {
		if ctx.Err() != nil {
			return built, ctx.Err()
		}
		if _, err := Rebuild(ctx, db, reader, p.tenant, p.year); err != nil {
			if first == nil {
				first = fmt.Errorf("арендатор %s, год %d: %w", p.tenant, p.year, err)
			}
			continue
		}
		built++
	}
	return built, first
}

// Run пересобирает кэш по расписанию до закрытия stop.
func Run(db *sql.DB, reader activityprojection.Reader, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		_, _ = RebuildAll(ctx, db, reader)
		cancel()
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}
