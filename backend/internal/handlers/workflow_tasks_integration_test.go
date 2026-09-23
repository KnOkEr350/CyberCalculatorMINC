package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/curators"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/tasks"
	"cybercalc/internal/testfixtures"
)

// SEC-04 и SEC-10 на реальной БД: цепочка fallback по ADR-22 — специалист,
// закреплённый куратор, ORG_ADMIN, SUPER_ADMIN; идемпотентная постановка,
// плановый пересчёт, границы арендатора и полномочия при выполнении.
func TestWorkflowTaskDispatcher(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	handlers := WorkflowTaskHandlers{DB: db}

	root, err := f.CreateUser(ctx, testfixtures.UserParams{Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization})
	if err != nil {
		t.Fatal(err)
	}
	newCompany := func() testfixtures.ITCompany {
		c, err := f.CreateITCompany(ctx, testfixtures.ITCompanyParams{CreatedBy: root.ID})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	company, foreign := newCompany(), newCompany()
	user := func(role models.Role, c testfixtures.ITCompany) testfixtures.User {
		t.Helper()
		u, err := f.CreateUser(ctx, testfixtures.UserParams{Role: role, EntityType: models.EntityOrganization, ITCompanyID: c.ID})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	university, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	partner, err := f.CreatePartner(ctx, company, university)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM workflow_tasks WHERE it_company_id IN ($1::uuid,$2::uuid)`, company.ID, foreign.ID)
	})
	orgAdmin, curator := user(models.RoleOrgAdmin, company), user(models.RoleCurator, company)
	foreignAdmin := user(models.RoleOrgAdmin, foreign)
	if _, err := curators.Assign(ctx, db, curators.NewInput{CuratorID: curator.ID, PartnerID: partner.ID,
		From: curators.Today(time.Now()), AssignedBy: root.ID}, time.Now()); err != nil {
		t.Fatal(err)
	}
	as := func(u testfixtures.User, c testfixtures.ITCompany) middleware.AuthUser {
		company := c.ID
		return middleware.AuthUser{ID: u.ID, Role: u.Role, EntityType: models.EntityOrganization, ITCompanyID: &company}
	}
	manager, foreignManager, curatorUser := as(orgAdmin, company), as(foreignAdmin, foreign), as(curator, company)

	create := func(u middleware.AuthUser, body string) (int, tasks.Task) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/workflow-tasks", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handlers.Create(rec, req, u)
		var task tasks.Task
		_ = json.Unmarshal(rec.Body.Bytes(), &task)
		return rec.Code, task
	}
	hrBody := func(subject string) string {
		return `{"kind":"internship.review","title":"Проверить комплект стажировки","subject_type":"entry","subject_id":"` + subject +
			`","partner_id":"` + partner.ID + `","required_role":"hr_specialist","required_permission":"entries.edit_internship"}`
	}
	post := func(u middleware.AuthUser, id, action, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/workflow-tasks/"+id+"/"+action, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if action == "reassign" {
			handlers.Reassign(rec, req, u, id)
		} else {
			handlers.Complete(rec, req, u, id)
		}
		return rec
	}
	events := func(id string) []tasks.Event {
		t.Helper()
		h, err := tasks.History(ctx, db, id)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	t.Run("права на постановку", func(t *testing.T) {
		if code, _ := create(curatorUser, hrBody("e-0-"+company.ID)); code != 403 {
			t.Fatalf("куратор не ставит задачи: %d", code)
		}
		if code, _ := create(manager, `{"kind":"x","title":"","subject_type":"entry","subject_id":"1","required_role":"hr_specialist","required_permission":"entries.edit_internship"}`); code != 400 {
			t.Fatalf("пустой заголовок: %d", code)
		}
		if code, _ := create(manager, strings.Replace(hrBody("e-0-"+company.ID), "entries.edit_internship", "made.up", 1)); code != 400 {
			t.Fatalf("неизвестное полномочие: %d", code)
		}
		other := `{"kind":"internship.review","title":"Чужой партнёр","subject_type":"entry","subject_id":"e-x-` + company.ID + `","partner_id":"` + partner.ID + `","required_role":"hr_specialist","required_permission":"entries.edit_internship"}`
		if code, _ := create(foreignManager, other); code != 400 {
			t.Fatalf("партнёр другого арендатора: %d", code)
		}
	})

	var fallbackTask tasks.Task
	t.Run("без специалиста задача уходит закреплённому куратору", func(t *testing.T) {
		code, task := create(manager, hrBody("e-1-"+company.ID))
		if code != 201 || task.Route != tasks.RouteCurator || task.AssigneeID != curator.ID || task.Escalated {
			t.Fatalf("fallback на куратора: %d %+v", code, task)
		}
		if !strings.Contains(task.FallbackReason, "hr_specialist") {
			t.Fatalf("причина fallback должна называть роль: %q", task.FallbackReason)
		}
		fallbackTask = task
		// Повторная постановка — та же задача, без нового события.
		code, again := create(manager, hrBody("e-1-"+company.ID))
		if code != 200 || again.ID != task.ID || len(events(task.ID)) != 1 {
			t.Fatalf("постановка идемпотентна: %d %+v событий %d", code, again, len(events(task.ID)))
		}
	})

	t.Run("куратор выполняет делегированное, но не чужое", func(t *testing.T) {
		// Юридическая задача куратору не делегируется: уходит к ORG_ADMIN.
		legal := `{"kind":"legal.check","title":"Проверить договор","subject_type":"entry","subject_id":"e-2-` + company.ID + `","partner_id":"` + partner.ID +
			`","required_role":"legal_specialist","required_permission":"tenant.read"}`
		code, task := create(manager, legal)
		if code != 201 || task.Route != tasks.RouteOrgAdmin || !task.Escalated || task.AssigneeID != orgAdmin.ID {
			t.Fatalf("неделегируемое уходит к ORG_ADMIN с пометкой: %d %+v", code, task)
		}
		if rec := post(curatorUser, task.ID, "complete", ""); rec.Code != 403 && rec.Code != 404 {
			t.Fatalf("куратор не закрывает чужую задачу: %d", rec.Code)
		}
		if rec := post(curatorUser, fallbackTask.ID, "complete", ""); rec.Code != 200 {
			t.Fatalf("куратор закрывает делегированную задачу: %d %s", rec.Code, rec.Body.String())
		}
		if rec := post(curatorUser, fallbackTask.ID, "complete", ""); rec.Code != 409 {
			t.Fatalf("повторное закрытие: %d", rec.Code)
		}
		// После закрытия та же задача может быть поставлена заново.
		if code, again := create(manager, hrBody("e-1-"+company.ID)); code != 201 || again.ID == fallbackTask.ID {
			t.Fatalf("после закрытия нужна новая задача: %d", code)
		}
	})

	t.Run("появился специалист — плановый пересчёт возвращает задачу ему", func(t *testing.T) {
		_, task := create(manager, hrBody("e-3-"+company.ID))
		if task.Route != tasks.RouteCurator {
			t.Fatalf("исходно куратор: %+v", task)
		}
		hr := user(models.RoleHRSpecialist, company)
		moved, err := tasks.RedispatchOpen(ctx, db, time.Now(), company.ID)
		if err != nil || moved < 1 {
			t.Fatalf("пересчёт должен переназначить: %d %v", moved, err)
		}
		got, _ := tasks.Get(ctx, db, task.ID)
		if got.Route != tasks.RouteSpecialist || got.AssigneeID != hr.ID || got.FallbackReason != "" {
			t.Fatalf("задача должна перейти к специалисту: %+v", got)
		}
		last := events(task.ID)
		if e := last[len(last)-1]; e.Action != "reassigned" || !e.System || e.ActorID != "" {
			t.Fatalf("плановое переназначение — действие системы: %+v", e)
		}
		var systemAudit int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='workflow_task' AND entity_id=$1 AND action='reassign' AND actor_type='system'`, task.ID).Scan(&systemAudit); err != nil || systemAudit != 1 {
			t.Fatalf("системное переназначение должно быть в аудите: %d %v", systemAudit, err)
		}
		// Идемпотентность: повторный пересчёт ничего не меняет.
		if again, err := tasks.RedispatchOpen(ctx, db, time.Now(), company.ID); err != nil || again != 0 {
			t.Fatalf("повторный пересчёт: %d %v", again, err)
		}
		if rec := post(as(hr, company), task.ID, "complete", ""); rec.Code != 200 {
			t.Fatalf("специалист закрывает свою задачу: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("куратор перестал быть закреплённым — задача эскалируется", func(t *testing.T) {
		// Кадровой службы в арендаторе больше нет, кроме появившейся выше — отключаем её.
		if _, err := db.ExecContext(ctx, `UPDATE users SET is_active=FALSE WHERE it_company_id::text=$1 AND role='hr_specialist'`, company.ID); err != nil {
			t.Fatal(err)
		}
		_, task := create(manager, hrBody("e-4-"+company.ID))
		if task.Route != tasks.RouteCurator {
			t.Fatalf("куратор ещё закреплён: %+v", task)
		}
		list, _ := curators.List(ctx, db, curators.Filter{CuratorID: curator.ID, ActiveOnly: true}, time.Now())
		if len(list) != 1 {
			t.Fatal("нужно действующее закрепление")
		}
		if _, err := curators.Revoke(ctx, db, list[0].ID, root.ID, "перевод", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := tasks.RedispatchOpen(ctx, db, time.Now(), company.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := tasks.Get(ctx, db, task.ID)
		if got.Route != tasks.RouteOrgAdmin || !got.Escalated || got.AssigneeID != orgAdmin.ID {
			t.Fatalf("без куратора задача уходит к ORG_ADMIN: %+v", got)
		}
		if !strings.Contains(got.FallbackReason, "нет действующего куратора") {
			t.Fatalf("причина эскалации: %q", got.FallbackReason)
		}
	})

	t.Run("видимость, ручное переназначение и граница арендатора", func(t *testing.T) {
		_, task := create(manager, hrBody("e-5-"+company.ID))
		if rec := post(foreignManager, task.ID, "reassign", `{"reason":"чужой"}`); rec.Code != 404 {
			t.Fatalf("чужой арендатор не переназначает: %d", rec.Code)
		}
		if rec := post(curatorUser, task.ID, "reassign", `{"reason":"я сам"}`); rec.Code != 403 {
			t.Fatalf("куратор не переназначает: %d", rec.Code)
		}
		if rec := post(manager, task.ID, "reassign", `{"reason":"  "}`); rec.Code != 400 {
			t.Fatalf("причина обязательна: %d", rec.Code)
		}
		before := len(events(task.ID))
		rec := post(manager, task.ID, "reassign", `{"reason":"проверка маршрута"}`)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"reassigned":false`) {
			t.Fatalf("исполнитель не изменился: %d %s", rec.Code, rec.Body.String())
		}
		if got := events(task.ID); len(got) != before+1 || got[len(got)-1].Action != "reassign_attempt" {
			t.Fatalf("попытка переназначения должна остаться в истории: %+v", got)
		}
		get := func(u middleware.AuthUser) int {
			rec := httptest.NewRecorder()
			handlers.Get(rec, httptest.NewRequest("GET", "/api/workflow-tasks/"+task.ID, nil), u, task.ID)
			return rec.Code
		}
		if get(manager) != 200 || get(foreignManager) != 404 || get(curatorUser) != 404 {
			t.Fatal("задачу видят управляющий арендатора и исполнитель, посторонние — нет")
		}
		listCount := func(u middleware.AuthUser, query string) int {
			rec := httptest.NewRecorder()
			handlers.List(rec, httptest.NewRequest("GET", "/api/workflow-tasks"+query, nil), u)
			var list []tasks.Task
			_ = json.Unmarshal(rec.Body.Bytes(), &list)
			return len(list)
		}
		if n := listCount(manager, "?escalated=1&status=open"); n < 2 {
			t.Fatalf("управляющий видит эскалированные задачи арендатора: %d", n)
		}
		if n := listCount(foreignManager, ""); n != 0 {
			t.Fatalf("чужой арендатор не видит задач: %d", n)
		}
		if n := listCount(curatorUser, "?status=open"); n != 0 {
			t.Fatalf("у куратора без закрепления своих задач нет: %d", n)
		}
	})

	t.Run("БД не принимает задачу с противоречивым состоянием", func(t *testing.T) {
		_, task := create(manager, hrBody("e-6-"+company.ID))
		if _, err := db.ExecContext(ctx, `UPDATE workflow_tasks SET route='specialist' WHERE id::text=$1`, task.ID); err == nil {
			t.Fatal("маршрут specialist без причины fallback и с пометкой эскалации должен отвергаться")
		}
		if _, err := db.ExecContext(ctx, `UPDATE workflow_task_events SET reason='иначе' WHERE task_id::text=$1`, task.ID); err == nil {
			t.Fatal("история задач не переписывается")
		}
	})
}
