package handlers

import (
	"context"
	"testing"

	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// OOP-01 на реальной БД: документ, действие, уровень, программа и эксперт —
// типизированные поля, которые БД выводит из проверенного payload и не даёт
// разойтись с ним.
func TestOopTypedModelFollowsThePayload(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	admin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.CreatePartner(ctx, company, university)
	if err != nil {
		t.Fatal(err)
	}
	agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID, PartnerIDs: []string{partner.ID}, CreatedBy: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	insert := func(category, payload string) (string, error) {
		var id string
		err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES($1,$2,$3,$4,'fact',$5,'vuz',$6::jsonb,100000,100000,'average',$7) RETURNING id::text`,
			category, partner.ID, agreement.ID, company.ID, year, payload, admin.ID).Scan(&id)
		return id, err
	}
	type model struct {
		doc, level, activity, program, expert, specialty string
		incomplete                                       bool
	}
	read := func(id string) model {
		t.Helper()
		var m model
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(oop_doc_type,''),COALESCE(oop_level,''),COALESCE(oop_activity,''),
			COALESCE(oop_program_name,''),COALESCE(oop_expert_name,''),COALESCE(oop_specialty_code,''),oop_incomplete
			FROM entries WHERE id::text=$1`, id).Scan(&m.doc, &m.level, &m.activity, &m.program, &m.expert, &m.specialty, &m.incomplete); err != nil {
			t.Fatal(err)
		}
		return m
	}

	id, err := insert("ood_rpd", `{"doc_type":" RPD ","level":"vo","activity_type":"Development","program_name":"  Базы данных ","expert_full_name":"Петров П. П.","specialty_code":"09.03.01"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := read(id), (model{"rpd", "vo", "development", "Базы данных", "Петров П. П.", "09.03.01", false}); got != want {
		t.Fatalf("поля должны выводиться из payload: %+v, ожидалось %+v", got, want)
	}

	t.Run("изменение payload меняет типизированные поля", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"doc_type":"oop","activity_type":"expertise"}' WHERE id::text=$1`, id); err != nil {
			t.Fatal(err)
		}
		if got := read(id); got.doc != "oop" || got.activity != "expertise" {
			t.Fatalf("поля разошлись с payload: %+v", got)
		}
	})

	t.Run("прямая запись в производные поля не действует", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `UPDATE entries SET oop_doc_type='rpd',oop_program_name='Подмена' WHERE id::text=$1`, id); err != nil {
			t.Fatal(err)
		}
		if got := read(id); got.doc != "oop" || got.program != "Базы данных" {
			t.Fatalf("производные поля обязаны следовать payload: %+v", got)
		}
	})

	t.Run("неполный состав помечается, а недопустимое значение не приводится", func(t *testing.T) {
		for name, payload := range map[string]string{
			"без вида документа":   `{"level":"vo","activity_type":"development","program_name":"X"}`,
			"неизвестное действие": `{"doc_type":"rpd","level":"vo","activity_type":"rewrite","program_name":"X"}`,
			"без программы":        `{"doc_type":"rpd","level":"vo","activity_type":"development","program_name":"  "}`,
			"пустой payload":       `{}`,
		} {
			incomplete, err := insert("ood_rpd", payload)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !read(incomplete).incomplete {
				t.Errorf("%s: запись должна быть помечена неполной", name)
			}
		}
		rewritten, _ := insert("ood_rpd", `{"doc_type":"rpd","level":"vo","activity_type":"rewrite","program_name":"X"}`)
		if got := read(rewritten); got.activity != "" {
			t.Errorf("недопустимое действие не должно попадать в колонку: %+v", got)
		}
		// Дополнение payload снимает отметку.
		if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"activity_type":"update"}' WHERE id::text=$1`, rewritten); err != nil {
			t.Fatal(err)
		}
		if got := read(rewritten); got.incomplete || got.activity != "update" {
			t.Errorf("после дополнения запись полная: %+v", got)
		}
	})

	t.Run("БД не позволяет разойтись отметке и составу даже без триггера", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, `ALTER TABLE entries DISABLE TRIGGER entries_oop_rpd_sync_trg`); err != nil {
			t.Fatal(err)
		}
		_, err := db.ExecContext(ctx, `UPDATE entries SET oop_doc_type=NULL WHERE id::text=$1`, id)
		if _, e := db.ExecContext(ctx, `ALTER TABLE entries ENABLE TRIGGER entries_oop_rpd_sync_trg`); e != nil {
			t.Fatal(e)
		}
		if err == nil {
			t.Fatal("полный состав с отметкой «полный» и пустым документом должен отвергаться ограничением")
		}
	})

	t.Run("у записей других видов колонок ООП нет", func(t *testing.T) {
		other, err := insert("teachers", `{"doc_type":"rpd","program_name":"не ООП"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got := read(other); got.doc != "" || got.program != "" {
			t.Fatalf("колонки ООП заполняются только у Вида 3: %+v", got)
		}
	})
}
