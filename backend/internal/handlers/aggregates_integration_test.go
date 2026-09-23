package handlers

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"cybercalc/internal/aggregates"
	reportrepository "cybercalc/internal/modules/reporting/repository"
	"cybercalc/internal/money"
	"cybercalc/internal/testfixtures"
)

// RISK-08 на реальной БД: кэш агрегатов совпадает с транзакционными таблицами,
// пересборка идемпотентна, арендаторы не смешиваются, устаревание видно.
func TestAggregatesCacheMatchesTransactionalTables(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	projection := reportrepository.NewActivityProjection(db)

	newTenant := func() tenantScenario { return newTenant(ctx, t, f) }
	own, other := newTenant(), newTenant()
	add := func(tn tenantScenario, category, period, audience string, amount int) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,'{}',$8,$8,'average',$9)`,
			category, tn.partner, tn.agreement, tn.company, period, year, audience, amount, tn.admin); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		add(own, "teachers", "plan", "vuz", 100000+i)
		add(own, "teachers", "fact", "vuz", 90000+i)
		add(own, "internship", "fact", "vuz", 30340)
		add(own, "ood_rpd", "plan", "kolledj", 55000) // без полного состава — учитывается как неполная
	}
	add(other, "teachers", "fact", "vuz", 777000)

	built, err := aggregates.Rebuild(ctx, db, projection, own.company, year)
	if err != nil || built == 0 {
		t.Fatalf("сборка: %d %v", built, err)
	}
	if _, err := aggregates.Rebuild(ctx, db, projection, other.company, year); err != nil {
		t.Fatal(err)
	}

	t.Run("суммы совпадают с entries", func(t *testing.T) {
		rows, err := aggregates.Read(ctx, db, own.company, year)
		if err != nil {
			t.Fatal(err)
		}
		var plan, fact money.Amount
		var planEntries, factEntries int
		for _, r := range rows {
			plan, _ = money.Add(plan, r.PlanAmount)
			fact, _ = money.Add(fact, r.FactAmount)
			planEntries += r.PlanEntries
			factEntries += r.FactEntries
		}
		// Независимый расчёт прямо по таблице, без проекции.
		var wantPlan, wantFact string
		var wantPlanN, wantFactN int
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(sum(amount_rub) FILTER(WHERE period_type='plan'),0)::text,
				COALESCE(sum(amount_rub) FILTER(WHERE period_type='fact'),0)::text,
				count(*) FILTER(WHERE period_type='plan'),count(*) FILTER(WHERE period_type='fact')
			FROM entries WHERE it_company_id::text=$1 AND report_year=$2`, own.company, year).Scan(&wantPlan, &wantFact, &wantPlanN, &wantFactN); err != nil {
			t.Fatal(err)
		}
		if plan.String() != wantPlan || fact.String() != wantFact || planEntries != wantPlanN || factEntries != wantFactN {
			t.Fatalf("кэш: план %s/%d, факт %s/%d; таблица: план %s/%d, факт %s/%d",
				plan, planEntries, fact, factEntries, wantPlan, wantPlanN, wantFact, wantFactN)
		}
	})

	t.Run("кэш точен, пока источник не менялся", func(t *testing.T) {
		mismatches, err := aggregates.Verify(ctx, db, projection, own.company, year)
		if err != nil || len(mismatches) != 0 {
			t.Fatalf("расхождения: %v %v", mismatches, err)
		}
	})

	t.Run("пересборка идемпотентна", func(t *testing.T) {
		before, _ := aggregates.Read(ctx, db, own.company, year)
		if _, err := aggregates.Rebuild(ctx, db, projection, own.company, year); err != nil {
			t.Fatal(err)
		}
		after, _ := aggregates.Read(ctx, db, own.company, year)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("повторная сборка изменила содержимое:\n%+v\n%+v", before, after)
		}
	})

	t.Run("устаревание видно, пересборка исправляет", func(t *testing.T) {
		add(own, "teachers", "fact", "vuz", 12345)
		mismatches, err := aggregates.Verify(ctx, db, projection, own.company, year)
		if err != nil || len(mismatches) == 0 {
			t.Fatalf("новая запись должна давать расхождение: %v %v", mismatches, err)
		}
		if _, err := aggregates.Rebuild(ctx, db, projection, own.company, year); err != nil {
			t.Fatal(err)
		}
		if mismatches, err := aggregates.Verify(ctx, db, projection, own.company, year); err != nil || len(mismatches) != 0 {
			t.Fatalf("после пересборки расхождений быть не должно: %v %v", mismatches, err)
		}
	})

	t.Run("арендаторы не смешиваются", func(t *testing.T) {
		rows, err := aggregates.Read(ctx, db, other.company, year)
		if err != nil || len(rows) != 1 || rows[0].FactAmount.String() != "777000.00" {
			t.Fatalf("срез соседа не должен зависеть от пересборки: %+v %v", rows, err)
		}
	})

	t.Run("параллельные пересборки не портят срез", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := aggregates.Rebuild(ctx, db, projection, own.company, year)
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if mismatches, err := aggregates.Verify(ctx, db, projection, own.company, year); err != nil || len(mismatches) != 0 {
			t.Fatalf("после гонки срез должен быть точным: %v %v", mismatches, err)
		}
	})

	t.Run("без утверждённого отчёта зачёт нулевой", func(t *testing.T) {
		rows, _ := aggregates.Read(ctx, db, own.company, year)
		for _, r := range rows {
			if r.CountedFact != 0 {
				t.Fatalf("без утверждённого отчёта в зачёт ничего не идёт: %+v", r)
			}
		}
	})
}
