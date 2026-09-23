package handlers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/tasks"
	"cybercalc/internal/testfixtures"
)

// SEC-10: события отчёта сами ставят и закрывают задачи. Проверяется на
// реальной БД вся цепочка ready → verified → approved и возврат в черновик.
func TestReportEventsDispatchAndCloseTasks(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())

	root, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	company, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: root.ID})
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
	agreement, err := f.CreateAgreement(ctx, testfixtures.AgreementParams{CompanyID: company.ID, PartnerIDs: []string{partner.ID}, CreatedBy: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	orgAdmin, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization, ITCompanyID: company.ID})
	if err != nil {
		t.Fatal(err)
	}
	curator, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: company.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := curators.Assign(ctx, db, curators.NewInput{CuratorID: curator.ID, PartnerID: partner.ID,
		From: curators.Today(time.Now()), AssignedBy: root.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM workflow_tasks WHERE it_company_id=$1::uuid`, company.ID)
	})
	actor := middleware.AuthUser{ID: orgAdmin.ID, Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization, ITCompanyID: &company.ID}
	subject := fmt.Sprintf("%s:%d:plan", agreement.ID, year)

	move := func(status string) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := syncReportTasks(ctx, tx, actor, agreement.ID, year, "plan", status); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	state := func(kind string) (status, assignee, route string, count int) {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT status,COALESCE(assignee_id::text,''),route FROM workflow_tasks WHERE kind=$1 AND subject_type='agreement_report' AND subject_id=$2 ORDER BY created_at`, kind, subject)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			count++
			if err := rows.Scan(&status, &assignee, &route); err != nil {
				t.Fatal(err)
			}
		}
		return
	}

	// «Готово» → задача проверки уходит куратору; повтор не плодит задач.
	move("ready")
	move("ready")
	if status, assignee, route, count := state(taskReportVerify); count != 1 || status != "open" || assignee != curator.ID || route != string(tasks.RouteSpecialist) {
		t.Fatalf("проверка после «готово»: задач %d, статус %s, исполнитель %s (ожидался куратор %s), маршрут %s", count, status, assignee, curator.ID, route)
	}
	// «Проверено» → проверка закрыта, утверждать должен администратор организации.
	move("verified")
	if status, _, _, _ := state(taskReportVerify); status != "done" {
		t.Fatalf("после проверки задача проверки должна быть выполнена, а она %s", status)
	}
	if status, assignee, _, count := state(taskReportApprove); count != 1 || status != "open" || assignee != orgAdmin.ID {
		t.Fatalf("утверждение после проверки: задач %d, статус %s, исполнитель %s (ожидался %s)", count, status, assignee, orgAdmin.ID)
	}
	// Возврат в черновик отменяет открытое утверждение, выполненная проверка остаётся выполненной.
	move("draft")
	if status, _, _, _ := state(taskReportApprove); status != "cancelled" {
		t.Fatalf("возврат в черновик должен отменить утверждение, а оно %s", status)
	}
	if status, _, _, _ := state(taskReportVerify); status != "done" {
		t.Fatalf("выполненная проверка не должна меняться: %s", status)
	}
	// Второй круг после доработки: новая проверка, затем утверждение.
	move("ready")
	move("verified")
	move("approved")
	if status, _, _, count := state(taskReportApprove); count != 2 || status != "done" {
		t.Fatalf("после утверждения обе задачи утверждения закрыты: задач %d, последняя %s", count, status)
	}
	var open int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workflow_tasks WHERE it_company_id=$1::uuid AND status='open'`, company.ID).Scan(&open); err != nil || open != 0 {
		t.Fatalf("открытых задач после утверждения быть не должно: %d %v", open, err)
	}
	var audited int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='workflow_task' AND action='create' AND user_id=$1::uuid`, orgAdmin.ID).Scan(&audited); err != nil || audited == 0 {
		t.Fatalf("постановка задачи должна попадать в аудит: %d %v", audited, err)
	}
}
