package handlers

import (
	"context"
	"testing"

	"cybercalc/internal/testfixtures"
)

// SCH-01 на реальной БД: программа/платформа, ссылки на соглашение, реестр групп
// и акт, период цифрового следа и источник финансирования выводятся из payload.
func TestSchoolTypedModelFollowsThePayload(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tn := newTenant(ctx, t, f)
	insert := func(category, audience, payload string) (string, error) {
		var id string
		err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES($1,$2,$3,$4,'fact',$5,$6,$7::jsonb,100000,100000,'average',$8) RETURNING id::text`,
			category, tn.partner, tn.agreement, tn.company, year, audience, payload, tn.admin).Scan(&id)
		return id, err
	}
	type row struct {
		program, classes, agreement, groups, act, start, end, source, budget, citizen string
		incomplete                                                                    bool
	}
	read := func(id string) row {
		t.Helper()
		var r row
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(school_program_name,''),COALESCE(school_class_range,''),COALESCE(school_agreement_reference,''),
			COALESCE(school_groups_reference,''),COALESCE(school_act_reference,''),COALESCE(school_period_start::text,''),COALESCE(school_period_end::text,''),
			COALESCE(school_funding_source,''),COALESCE(school_budget_funding,''),COALESCE(school_citizen_funding,''),school_incomplete
			FROM entries WHERE id::text=$1`, id).Scan(&r.program, &r.classes, &r.agreement, &r.groups, &r.act, &r.start, &r.end, &r.source, &r.budget, &r.citizen, &r.incomplete); err != nil {
			t.Fatal(err)
		}
		return r
	}

	clubs, err := insert("it_clubs", "school", `{"program_name":"  Робототехника ","class_range":"5-7","school_agreement_reference":"Соглашение № 4","participant_groups_reference":"Реестр 2","acceptance_act_reference":"Акт 9","funding_source":"Средства компании","budget_funding":"Absent","citizen_funding":"absent"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(clubs); got != (row{"Робототехника", "5-7", "Соглашение № 4", "Реестр 2", "Акт 9", "", "", "Средства компании", "absent", "absent", false}) {
		t.Fatalf("Вид 6: %+v", got)
	}

	content, err := insert("edu_content", "school", `{"platform_name":"Платформа","digital_trace_period_start":"2026-01-01","digital_trace_period_end":"2026-04-30"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(content); got.program != "Платформа" || got.start != "2026-01-01" || got.end != "2026-04-30" || got.incomplete {
		t.Fatalf("Вид 8 берёт платформу и период: %+v", got)
	}

	t.Run("изменение payload меняет типизированные поля, прямая запись не действует", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"acceptance_act_reference":"Акт 10"}' WHERE id::text=$1`, clubs); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE entries SET school_program_name='Подмена',school_act_reference='Подмена' WHERE id::text=$1`, clubs); err != nil {
			t.Fatal(err)
		}
		if got := read(clubs); got.act != "Акт 10" || got.program != "Робототехника" {
			t.Fatalf("производные поля обязаны следовать payload: %+v", got)
		}
	})

	t.Run("недопустимое не приводится, неполное помечается", func(t *testing.T) {
		bad, err := insert("teacher_training", "school", `{"budget_funding":"maybe","digital_trace_period_start":"вчера"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := read(bad); !got.incomplete || got.budget != "" || got.start != "" {
			t.Fatalf("недопустимые значения не попадают в колонки, программа не названа: %+v", got)
		}
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"program_name":"Курс"}' WHERE id::text=$1`, bad); err != nil {
			t.Fatal(err)
		}
		if got := read(bad); got.incomplete || got.program != "Курс" {
			t.Fatalf("после дополнения запись полная: %+v", got)
		}
	})

	t.Run("у других видов колонок школы нет", func(t *testing.T) {
		other, err := insert("teachers", "vuz", `{"program_name":"не школа","acceptance_act_reference":"акт"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := read(other); got.program != "" || got.act != "" || got.incomplete {
			t.Fatalf("колонки школы заполняются только у Видов 6–8: %+v", got)
		}
	})

	t.Run("БД не позволяет разойтись отметке и составу даже без триггера", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `ALTER TABLE entries DISABLE TRIGGER entries_school_sync_trg`); err != nil {
			t.Fatal(err)
		}
		_, err := db.ExecContext(ctx, `UPDATE entries SET school_program_name=NULL WHERE id::text=$1`, clubs)
		if _, e := db.ExecContext(ctx, `ALTER TABLE entries ENABLE TRIGGER entries_school_sync_trg`); e != nil {
			t.Fatal(e)
		}
		if err == nil {
			t.Fatal("отметка «полная» при пустой программе должна отвергаться ограничением")
		}
	})
}
