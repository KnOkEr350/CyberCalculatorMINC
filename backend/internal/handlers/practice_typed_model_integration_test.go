package handlers

import (
	"context"
	"fmt"
	"testing"

	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// PRA-01 / PRA-06: карточка практиканта живёт отдельно от стажировки,
// синхронизируется с совместимым payload и не теряет неполные legacy-строки.
func TestPracticeTypedCardAndLegacyBackfill(t *testing.T) {
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
	var mentorID string
	if err := db.QueryRowContext(ctx, `INSERT INTO mentors(partner_id,full_name) VALUES($1,'Ильин Сергей Викторович') RETURNING id::text`, partner.ID).Scan(&mentorID); err != nil {
		t.Fatal(err)
	}

	fullPayload := func(student, course string) string {
		return fmt.Sprintf(`{"student_full_name":%q,"specialty_code":"09.03.01","course":%q,
			"period_start":"2026-06-01","period_end":"2026-07-31","labor_contract_type":"fixed_term",
			"labor_contract_number":"88-ТД","labor_contract_date":"2026-05-30",
			"practice_agreement_number":"ПР-12","practice_agreement_date":"2026-05-15",
			"practice_agreement_start_date":"2026-06-01","practice_agreement_end_date":"2026-07-31",
			"student_age":19,"weekly_hours":30}`, student, course)
	}
	insert := func(category, payload string, withMentor bool) (string, error) {
		var id string
		mentor, start, end, order, orderDate, student := "", "", "", "", "", ""
		if withMentor {
			mentor, start, end, order, orderDate, student = mentorID, "2026-06-01", "2026-07-31", "12-ОК", "2026-05-28", "громов алексей михайлович"
		}
		err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by,mentor_id,mentor_assignment_start,
			mentor_assignment_end,mentor_order_number,mentor_order_date,assigned_student_name)
			VALUES($1,$2,$3,$4,'fact',$5,'vuz',$6::jsonb,1,1,'average',$7,NULLIF($8,'')::uuid,
			NULLIF($9,'')::date,NULLIF($10,'')::date,NULLIF($11,''),NULLIF($12,'')::date,NULLIF($13,'')) RETURNING id::text`,
			category, partner.ID, agreement.ID, company.ID, year, payload, admin.ID,
			mentor, start, end, order, orderDate, student).Scan(&id)
		return id, err
	}

	practiceID, err := insert("employment_practice", fullPayload("Громов Алексей Михайлович", "3"), true)
	if err != nil {
		t.Fatal(err)
	}
	var student, specialty, course, status string
	var age int
	var hours float64
	if err := db.QueryRowContext(ctx, `SELECT student_full_name,specialty_code,course,student_age,weekly_hours::float8,backfill_status
		FROM practice_records WHERE entry_id::text=$1`, practiceID).Scan(&student, &specialty, &course, &age, &hours, &status); err != nil {
		t.Fatal(err)
	}
	if student != "Громов Алексей Михайлович" || specialty != "09.03.01" || course != "3" || age != 19 || hours != 30 || status != "complete" {
		t.Fatalf("unexpected typed practice card: student=%q specialty=%q course=%q age=%d hours=%g status=%q", student, specialty, course, age, hours, status)
	}

	if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=payload||'{"course":"4"}' WHERE id::text=$1`, practiceID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT course FROM practice_records WHERE entry_id::text=$1`, practiceID).Scan(&course); err != nil || course != "4" {
		t.Fatalf("typed card did not follow payload: course=%q err=%v", course, err)
	}

	legacyID, err := insert("employment_practice", `{"student_full_name":"Неполная запись"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var openFindings int
	if err := db.QueryRowContext(ctx, `SELECT p.backfill_status,count(f.id) FILTER(WHERE f.resolved_at IS NULL)
		FROM practice_records p LEFT JOIN legacy_backfill_findings f ON f.entry_id=p.entry_id AND f.rule_code='practice.model_incomplete'
		WHERE p.entry_id::text=$1 GROUP BY p.backfill_status`, legacyID).Scan(&status, &openFindings); err != nil {
		t.Fatal(err)
	}
	if status != "manual_review" || openFindings != 1 {
		t.Fatalf("incomplete legacy row must remain for review: status=%q findings=%d", status, openFindings)
	}

	if _, err := db.ExecContext(ctx, `UPDATE entries SET payload=$2::jsonb,mentor_id=$3,mentor_assignment_start='2026-06-01',
		mentor_assignment_end='2026-07-31',mentor_order_number='12-ОК',mentor_order_date='2026-05-28',assigned_student_name='неполная запись'
		WHERE id::text=$1`, legacyID, fullPayload("Неполная запись", "3"), mentorID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT p.backfill_status,count(f.id) FILTER(WHERE f.resolved_at IS NULL)
		FROM practice_records p LEFT JOIN legacy_backfill_findings f ON f.entry_id=p.entry_id AND f.rule_code='practice.model_incomplete'
		WHERE p.entry_id::text=$1 GROUP BY p.backfill_status`, legacyID).Scan(&status, &openFindings); err != nil {
		t.Fatal(err)
	}
	if status != "complete" || openFindings != 0 {
		t.Fatalf("completed card must resolve finding: status=%q findings=%d", status, openFindings)
	}

	internshipID, err := insert("internship", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM practice_records WHERE entry_id::text=$1`, internshipID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("internship leaked into practice table: count=%d err=%v", count, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO practice_records(entry_id,partner_id,agreement_id,backfill_status)
		VALUES($1,$2,$3,'manual_review')`, internshipID, partner.ID, agreement.ID); err == nil {
		t.Fatal("practice table accepted an internship entry")
	}
}
