package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/testfixtures"
)

// Назначение наставника студенту на те же даты обязано жить и в плане, и в
// факте; запрет пересечения действует только внутри одного среза.
func TestMentorAssignmentAllowedInPlanAndFactButNotTwiceInOne(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	var mentor string
	if err := db.QueryRowContext(ctx, `INSERT INTO mentors(partner_id,full_name) VALUES($1,'Иванов Иван Иванович') RETURNING id::text`, tenant.partner).Scan(&mentor); err != nil {
		t.Fatal(err)
	}
	insert := func(period string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by,mentor_id,assigned_student_name,
			mentor_assignment_start,mentor_assignment_end,mentor_order_number,mentor_order_date)
			VALUES('internship',$1,$2,$3,$4,$5,'vuz','{}',100,100,'average',$6,$7,'Сидоров Сидор','2026-01-01','2026-01-31','1-ОК','2025-12-30')`,
			tenant.partner, tenant.agreement, tenant.company, period, year, tenant.admin, mentor)
		return err
	}
	if err := insert("plan"); err != nil {
		t.Fatalf("план: %v", err)
	}
	if err := insert("fact"); err != nil {
		t.Fatalf("факт по тому же назначению должен приниматься: %v", err)
	}
	if err := insert("plan"); err == nil {
		t.Fatal("второе пересекающееся назначение в плане должно отклоняться")
	}

	handlers := EntryHandlers{DB: db}
	request := httptest.NewRequest("GET", "/api/entries", nil)
	payload := func() map[string]interface{} {
		return map[string]interface{}{"mentor_id": mentor, "student_full_name": "Сидоров Сидор",
			"mentor_assignment_start": "2026-01-10", "mentor_assignment_end": "2026-01-20"}
	}
	for _, period := range []string{"plan", "fact"} {
		if err := handlers.validateMentor(request, "internship", period, tenant.partner, "", payload()); err == nil {
			t.Fatalf("пересечение внутри среза %s должно отклоняться", period)
		}
	}
	for name, dates := range map[string][2]string{
		"окончание раньше начала": {"2026-06-01", "2026-05-01"},
		"опечатка в годе":         {"202026-05-01", "2026-05-31"},
		"не ISO-формат":           {"01.05.2026", "31.05.2026"},
	} {
		bad := payload()
		bad["mentor_assignment_start"], bad["mentor_assignment_end"] = dates[0], dates[1]
		err := handlers.validateMentor(request, "internship", "plan", tenant.partner, "", bad)
		if err == nil || strings.Contains(err.Error(), "не удалось проверить") {
			t.Fatalf("%s: ожидалось понятное сообщение о датах, получено %v", name, err)
		}
	}
	other := payload()
	other["mentor_assignment_start"], other["mentor_assignment_end"] = "2026-03-01", "2026-03-31"
	if err := handlers.validateMentor(request, "internship", "fact", tenant.partner, "", other); err != nil {
		t.Fatalf("непересекающийся период должен приниматься: %v", err)
	}
}
