package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

type processCall struct {
	handlers RegulatoryHandlers
}

func (c processCall) create(user middleware.AuthUser, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest("POST", "/api/regulatory/processes", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c.handlers.Create(recorder, request, user)
	return recorder
}

func (c processCall) act(user middleware.AuthUser, id, action, reason string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"action":%q,"reason":%q}`, action, reason)
	request := httptest.NewRequest("POST", "/api/regulatory/processes/"+id+"/actions", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c.handlers.Act(recorder, request, user, id)
	return recorder
}

func (c processCall) get(user middleware.AuthUser, id string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	c.handlers.Get(recorder, httptest.NewRequest("GET", "/api/regulatory/processes/"+id, nil), user, id)
	return recorder
}

func decodeProcess(t *testing.T, recorder *httptest.ResponseRecorder) processView {
	t.Helper()
	var view processView
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatalf("ответ не разбирается: %v (%s)", err, recorder.Body.String())
	}
	return view
}

// WF-01 на реальной БД: цепочка соглашения через API с разными сторонами.
// ИТ-организация отправляет и дорабатывает, образовательная организация
// соглашения рассматривает; чужие профили процесса не видят.
func TestRegulatoryProcessThroughTheAPI(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	foreign := newTenant(ctx, t, f)
	call := processCall{handlers: RegulatoryHandlers{DB: db}}

	author := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	reviewer := middleware.AuthUser{ID: tenant.admin, Role: models.RoleCurator,
		EntityType: models.EntityEduInst, PartnerID: &tenant.partner}
	// Образовательная организация другой ИТ-компании: к этому соглашению отношения не имеет.
	outsider := middleware.AuthUser{ID: foreign.admin, Role: models.RoleCurator,
		EntityType: models.EntityEduInst, PartnerID: &foreign.partner}
	foreignAdmin := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
	hr := middleware.AuthUser{ID: tenant.admin, Role: models.RoleHRSpecialist,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	body := fmt.Sprintf(`{"kind":"agreement","agreement_id":%q,"report_year":%d}`, tenant.agreement, year)

	t.Run("заводит только ИТ-организация соглашения", func(t *testing.T) {
		for name, user := range map[string]middleware.AuthUser{
			"кадровая служба":       hr,
			"образовательная орг.":  reviewer,
			"чужая ИТ-компания":     foreignAdmin,
			"чужая образовательная": outsider,
		} {
			if got := call.create(user, body); got.Code != 403 {
				t.Errorf("%s не должна заводить процесс, получено %d: %s", name, got.Code, got.Body.String())
			}
		}
		if got := call.create(author, `{"kind":"нет","agreement_id":"`+tenant.agreement+`","report_year":2026}`); got.Code != 400 {
			t.Errorf("неизвестный вид: %d", got.Code)
		}
		if got := call.create(author, `{"kind":"agreement","agreement_id":"00000000-0000-0000-0000-000000000000","report_year":2026}`); got.Code != 404 {
			t.Errorf("несуществующее соглашение: %d", got.Code)
		}
	})

	created := call.create(author, body)
	if created.Code != 201 {
		t.Fatalf("создание вернуло %d: %s", created.Code, created.Body.String())
	}
	process := decodeProcess(t, created)
	if process.Status != "draft" || len(process.Allowed) != 1 || process.Allowed[0] != "send" {
		t.Fatalf("новый процесс: %+v", process)
	}
	if again := call.create(author, body); again.Code != 200 || decodeProcess(t, again).ID != process.ID {
		t.Fatalf("повторное создание должно вернуть тот же процесс: %d", again.Code)
	}

	t.Run("права на действия зависят от стороны", func(t *testing.T) {
		// Рецензент не отправляет, автор не рассматривает от своего имени без основания.
		if got := call.act(reviewer, process.ID, "send", ""); got.Code != 403 {
			t.Errorf("образовательная организация не отправляет: %d", got.Code)
		}
		if got := call.act(hr, process.ID, "send", ""); got.Code != 403 {
			t.Errorf("кадровая служба не отправляет: %d", got.Code)
		}
		// Чужие профили процесс не видят вовсе: он неотличим от несуществующего.
		for name, user := range map[string]middleware.AuthUser{"чужая ИТ-компания": foreignAdmin, "чужая образовательная": outsider} {
			if got := call.get(user, process.ID); got.Code != 404 {
				t.Errorf("%s: процесс должен быть скрыт, получено %d", name, got.Code)
			}
			if got := call.act(user, process.ID, "approve", "основание"); got.Code != 404 {
				t.Errorf("%s: действие над чужим процессом %d", name, got.Code)
			}
		}
	})

	if got := call.act(author, process.ID, "send", ""); got.Code != 200 {
		t.Fatalf("отправка: %d %s", got.Code, got.Body.String())
	}
	sent := decodeProcess(t, call.get(author, process.ID))
	if sent.Status != "sent" || sent.Window != "review" || sent.DueDate == "" || sent.DaysLeft == nil || *sent.DaysLeft != 14 {
		t.Fatalf("после отправки: %+v", sent)
	}
	if sent.SentLate {
		t.Fatal("у проекта соглашения нет срока отправки")
	}

	t.Run("допустимые действия видны по ролям", func(t *testing.T) {
		if got := decodeProcess(t, call.get(author, process.ID)).Allowed; strings.Join(got, ",") != "start_review,approve,request_rework" {
			// Автор — суперадминистратор: он может зафиксировать ответ за контрагента.
			t.Errorf("допустимые действия автора-администратора: %v", got)
		}
		if got := decodeProcess(t, call.get(reviewer, process.ID)).Allowed; strings.Join(got, ",") != "start_review,approve,request_rework" {
			t.Errorf("допустимые действия рецензента: %v", got)
		}
	})

	t.Run("ответ за контрагента требует основания", func(t *testing.T) {
		if got := call.act(author, process.ID, "approve", ""); got.Code != 400 {
			t.Errorf("согласование сотрудником ИТ-организации без основания: %d", got.Code)
		}
	})

	if got := call.act(reviewer, process.ID, "request_rework", ""); got.Code != 400 {
		t.Fatalf("возврат без замечаний: %d %s", got.Code, got.Body.String())
	}
	if got := call.act(reviewer, process.ID, "request_rework", "не хватает приложения № 2"); got.Code != 200 {
		t.Fatalf("возврат на доработку: %d %s", got.Code, got.Body.String())
	}
	rework := decodeProcess(t, call.get(reviewer, process.ID))
	if rework.Status != "rework" || rework.Remarks != "не хватает приложения № 2" || rework.DaysLeft == nil || *rework.DaysLeft != 10 {
		t.Fatalf("после возврата: %+v", rework)
	}
	// Возвращённый процесс рецензент больше не «ведёт»: остаётся действие автора.
	if got := decodeProcess(t, call.get(reviewer, process.ID)).Allowed; len(got) != 0 {
		t.Errorf("у рецензента в доработке нет действий: %v", got)
	}
	if got := call.act(reviewer, process.ID, "approve", ""); got.Code != 409 {
		t.Errorf("согласование процесса на доработке: %d", got.Code)
	}
	if got := call.act(author, process.ID, "resubmit", ""); got.Code != 200 {
		t.Fatalf("повторная отправка: %d %s", got.Code, got.Body.String())
	}
	if got := call.act(reviewer, process.ID, "approve", ""); got.Code != 200 {
		t.Fatalf("согласование: %d %s", got.Code, got.Body.String())
	}
	done := decodeProcess(t, call.get(author, process.ID))
	if done.Status != "approved" || len(done.Allowed) != 0 || len(done.Events) != 4 {
		t.Fatalf("итог: %+v", done)
	}
	if got := call.act(author, process.ID, "approve", "основание"); got.Code != 409 {
		t.Errorf("действие над завершённым процессом: %d", got.Code)
	}

	// Каждый переход попал в журнал аудита вместе с автором.
	var audited int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE entity_type='regulatory_process' AND entity_id::text=$1`,
		process.ID).Scan(&audited); err != nil {
		t.Fatal(err)
	}
	if audited != 4 {
		t.Fatalf("в журнале аудита %d записей о переходах, ожидалось 4", audited)
	}
}

// WF-04: до среза предварительный перечень не отправляется — отказ понятный,
// а не ошибка сервера.
func TestPreliminaryListCannotBeSentBeforeItsCutoff(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	call := processCall{handlers: RegulatoryHandlers{DB: db}}
	author := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}

	// Срез 2099 года заведомо в будущем, поэтому тест не зависит от даты запуска.
	created := call.create(author, fmt.Sprintf(`{"kind":"preliminary","agreement_id":%q,"report_year":2099}`, tenant.agreement))
	if created.Code != 201 {
		t.Fatalf("создание: %d %s", created.Code, created.Body.String())
	}
	process := decodeProcess(t, created)
	if process.Cutoff != "2099-11-01" || process.SendDue != "2099-11-10" {
		t.Fatalf("контрольные даты в ответе: %+v", process)
	}
	got := call.act(author, process.ID, "send", "")
	if got.Code != 400 || !strings.Contains(got.Body.String(), "формируется на 01.11.2099") {
		t.Fatalf("отправка до среза: %d %s", got.Code, got.Body.String())
	}
	if decodeProcess(t, call.get(author, process.ID)).Status != "draft" {
		t.Fatal("отклонённая отправка не должна менять состояние")
	}
}

// Оспаривание — только после доработки и только с причиной.
func TestDisputeRequiresReworkAndReason(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)
	call := processCall{handlers: RegulatoryHandlers{DB: db}}
	author := middleware.AuthUser{ID: tenant.admin, Role: models.RoleSuperAdmin,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company}
	reviewer := middleware.AuthUser{ID: tenant.admin, Role: models.RoleCurator,
		EntityType: models.EntityEduInst, PartnerID: &tenant.partner}

	process := decodeProcess(t, call.create(author, fmt.Sprintf(`{"kind":"agreement","agreement_id":%q,"report_year":%d}`, tenant.agreement, year)))
	call.act(author, process.ID, "send", "")
	if got := call.act(reviewer, process.ID, "dispute", "не согласны"); got.Code != 409 {
		t.Fatalf("оспаривание при первичном рассмотрении: %d", got.Code)
	}
	call.act(reviewer, process.ID, "request_rework", "замечание")
	call.act(author, process.ID, "resubmit", "")
	if got := call.act(reviewer, process.ID, "dispute", ""); got.Code != 400 {
		t.Fatalf("оспаривание без причины: %d", got.Code)
	}
	if got := call.act(reviewer, process.ID, "dispute", "замечание не устранено"); got.Code != 200 {
		t.Fatalf("оспаривание с причиной: %d %s", got.Code, got.Body.String())
	}
	if got := decodeProcess(t, call.get(author, process.ID)); got.Status != "disputed" || len(got.Allowed) != 0 {
		t.Fatalf("итог: %+v", got)
	}
	if got := call.act(reviewer, process.ID, "нет-такого", ""); got.Code != 400 {
		t.Fatalf("неизвестное действие: %d", got.Code)
	}
}
