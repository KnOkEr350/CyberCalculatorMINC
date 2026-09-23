package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"cybercalc/internal/testfixtures"
)

// TCH-01 на реальной БД: нагрузка определяется ключом «сотрудник + учебное
// заведение + дисциплина + год + период». Повтор того же ключа не принимается,
// а различие в любой его части — это уже другая нагрузка.
func TestTeachingLoadAtomicKeyRejectsDuplicates(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	// Второе учебное заведение той же компании и второй сотрудник: нужны,
	// чтобы отличить дубль от законной другой строки.
	otherUniversity, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	otherPartner, err := f.CreatePartner(ctx, testfixtures.ITCompany{ID: tenant.company}, otherUniversity)
	if err != nil {
		t.Fatal(err)
	}
	otherAgreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: tenant.company,
		PartnerIDs: []string{otherPartner.ID}, CreatedBy: tenant.admin})
	if err != nil {
		t.Fatal(err)
	}
	staff := "11111111-1111-1111-1111-111111111111"
	otherStaff := "22222222-2222-2222-2222-222222222222"

	insert := func(partner, staffID, course, semester, period string, reportYear int) error {
		payload := fmt.Sprintf(`{"staff_member_id":%q,"course_name":%q,"semester":%s,"academic_hours":64}`,
			staffID, course, semester)
		// Соглашение обязано покрывать учебное заведение строки.
		agreement := tenant.agreement
		if partner == otherPartner.ID {
			agreement = otherAgreement.ID
		}
		_, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,$4,$5,'vuz',$6::jsonb,264960,264960,'average',$7)`,
			partner, agreement, tenant.company, period, reportYear, payload, tenant.admin)
		return err
	}

	if err := insert(tenant.partner, staff, "Разработка ПО", "3", "plan", year); err != nil {
		t.Fatalf("первая нагрузка должна сохраняться: %v", err)
	}

	t.Run("повтор того же ключа не принимается", func(t *testing.T) {
		err := insert(tenant.partner, staff, "Разработка ПО", "3", "plan", year)
		if err == nil {
			t.Fatal("та же нагрузка не должна вноситься дважды")
		}
		if !strings.Contains(err.Error(), "teaching_load_atomic_key_idx") {
			t.Fatalf("сработало не то ограничение: %v", err)
		}
	})

	t.Run("название дисциплины нормализуется", func(t *testing.T) {
		// Тот же курс с другим регистром и лишними пробелами — это не новая
		// дисциплина, иначе ключ обходился бы опечаткой.
		if err := insert(tenant.partner, staff, "  разработка   ПО ", "3", "plan", year); err == nil {
			t.Fatal("различие только в регистре и пробелах не создаёт новую нагрузку")
		}
	})

	t.Run("различие в любой части ключа даёт другую нагрузку", func(t *testing.T) {
		cases := []struct {
			name                          string
			partner, staffID, course, sem string
			period                        string
			reportYear                    int
		}{
			{"другое учебное заведение", otherPartner.ID, staff, "Разработка ПО", "3", "plan", year},
			{"другой сотрудник", tenant.partner, otherStaff, "Разработка ПО", "3", "plan", year},
			{"другая дисциплина", tenant.partner, staff, "Базы данных", "3", "plan", year},
			{"другой семестр", tenant.partner, staff, "Разработка ПО", "4", "plan", year},
			{"другой период", tenant.partner, staff, "Разработка ПО", "3", "fact", year},
			{"другой год", tenant.partner, staff, "Разработка ПО", "3", "plan", year - 1},
		}
		for _, tc := range cases {
			if err := insert(tc.partner, tc.staffID, tc.course, tc.sem, tc.period, tc.reportYear); err != nil {
				t.Errorf("%s: должна сохраняться как отдельная нагрузка: %v", tc.name, err)
			}
		}
	})

	t.Run("строки без сотрудника или дисциплины не блокируют друг друга", func(t *testing.T) {
		// Черновик ещё не описывает конкретную нагрузку: два таких черновика
		// не считаются дублем.
		draft := func() error {
			_, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
				audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
				VALUES('teachers',$1,$2,$3,'plan',$4,'vuz','{"academic_hours":64}',264960,264960,'average',$5)`,
				tenant.partner, tenant.agreement, tenant.company, year+1, tenant.admin)
			return err
		}
		if err := draft(); err != nil {
			t.Fatal(err)
		}
		if err := draft(); err != nil {
			t.Fatalf("незаполненные строки не должны попадать в ключ: %v", err)
		}
	})

	t.Run("ключ не выходит за пределы арендатора", func(t *testing.T) {
		foreign := newTenant(ctx, t, f)
		payload := `{"staff_member_id":"11111111-1111-1111-1111-111111111111","course_name":"Разработка ПО","semester":3,"academic_hours":64}`
		if _, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES('teachers',$1,$2,$3,'plan',$4,'vuz',$5::jsonb,264960,264960,'average',$6)`,
			foreign.partner, foreign.agreement, foreign.company, year, payload, foreign.admin); err != nil {
			t.Fatalf("та же нагрузка у другой ИТ-компании — её собственная: %v", err)
		}
	})

}
