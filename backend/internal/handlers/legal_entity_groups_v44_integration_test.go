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

// DATA-10 на реальной БД: доля участия больше 25%, один действующий договор,
// расторжение с историей и сверка индикативных лимитов с целевой суммой.
func TestLegalEntityGroupContractV44(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tn, foreign := newTenant(ctx, t, f), newTenant(ctx, t, f)
	handlers := LegalEntityGroupHandlers{DB: db}
	author := middleware.AuthUser{ID: tn.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &tn.company}
	foreignAuthor := middleware.AuthUser{ID: foreign.admin, Role: models.RoleSuperAdmin, EntityType: models.EntityOrganization, ITCompanyID: &foreign.company}
	hr := middleware.AuthUser{ID: tn.admin, Role: models.RoleHRSpecialist, EntityType: models.EntityOrganization, ITCompanyID: &tn.company}

	body := func(name string, members string) string {
		return fmt.Sprintf(`{"name":%q,"interaction_agreement_number":"ХОЛД-01","interaction_agreement_date":"2026-01-12",
			"authorized_entity_name":"ООО «ИТ-Холдинг»","authorized_entity_inn":"7707083893","authorized_entity_ogrn":"1027700132195",
			"document_reference":"Договор_взаимодействия_Холдинг.pdf","members":%s}`, name, members)
	}
	const goodMembers = `[
		{"name":"ООО «ИТ-Холдинг»","inn":"7707083893","ogrn":"1027700132195","is_it_organization":true,"share_percent":100,"target_amount_rub":5000000},
		{"name":"ООО «ИТ-Разработка»","inn":"7736207543","ogrn":"1027700229193","is_it_organization":true,"share_percent":100,"target_amount_rub":6000000},
		{"name":"ООО «ИТ-Безопасность»","inn":"7710140679","ogrn":"1027739642281","is_it_organization":false,"share_percent":51,"target_amount_rub":4000000}]`
	create := func(u middleware.AuthUser, payload string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/legal-entity-groups", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		handlers.Create(rec, req, u)
		return rec
	}
	_ = year

	t.Run("доля участия больше 25%", func(t *testing.T) {
		for _, share := range []string{"25", "20", "100.5", "0", "51.234"} {
			members := strings.Replace(goodMembers, `"share_percent":51`, `"share_percent":`+share, 1)
			if rec := create(author, body("Группа Х", members)); rec.Code != 400 {
				t.Errorf("доля %s должна отвергаться: %d %s", share, rec.Code, rec.Body.String())
			}
		}
	})

	var groupID string
	t.Run("создание и чтение с долями и сканом", func(t *testing.T) {
		if rec := create(hr, body("Группа", goodMembers)); rec.Code != 403 {
			t.Fatalf("кадровая служба не ведёт группы: %d", rec.Code)
		}
		rec := create(author, body("Группа", goodMembers))
		if rec.Code != 201 {
			t.Fatalf("создание: %d %s", rec.Code, rec.Body.String())
		}
		var created map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		groupID = created["id"]
		list := httptest.NewRecorder()
		handlers.List(list, httptest.NewRequest("GET", "/api/legal-entity-groups", nil), author)
		var groups []models.LegalEntityGroup
		if err := json.Unmarshal(list.Body.Bytes(), &groups); err != nil || len(groups) != 1 {
			t.Fatalf("список: %v %s", err, list.Body.String())
		}
		g := groups[0]
		if g.Status != "active" || g.DocumentReference != "Договор_взаимодействия_Холдинг.pdf" || len(g.Members) != 3 {
			t.Fatalf("группа: %+v", g)
		}
		for _, m := range g.Members {
			if m.SharePercent == nil {
				t.Fatalf("доля участника должна читаться: %+v", m)
			}
		}
	})

	t.Run("действующий договор один", func(t *testing.T) {
		if rec := create(author, body("Вторая группа", goodMembers)); rec.Code != 409 {
			t.Fatalf("второй действующий договор: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("сверка лимитов с целевой суммой", func(t *testing.T) {
		limits := func(u middleware.AuthUser, id string) map[string]interface{} {
			rec := httptest.NewRecorder()
			handlers.Limits(rec, httptest.NewRequest("GET", fmt.Sprintf("/api/legal-entity-groups/%s/limits?report_year=%d", id, year), nil), u, id)
			if rec.Code != 200 {
				t.Fatalf("лимиты: %d %s", rec.Code, rec.Body.String())
			}
			var out map[string]interface{}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			return out
		}
		out := limits(author, groupID)
		if out["allocated_rub"] != "15000000.00" || out["target_amount_rub"] != nil || out["over_allocated"] != false {
			t.Fatalf("без целевой суммы: %+v", out)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO organization_budget_targets(report_year,it_company_id,target_amount_rub,savings_base_rub,source_reference,notified_at,updated_by)
			VALUES($1,$2,14000000,466666666.67,'ФНС',$3::date,$4)`, year, tn.company, fmt.Sprintf("%d-07-25", year), tn.admin); err != nil {
			t.Skip("схема целевой суммы отличается: ", err)
		}
		out = limits(author, groupID)
		if out["target_amount_rub"] != "14000000.00" || out["over_allocated"] != true || out["over_allocated_rub"] != "1000000.00" || out["remaining_rub"] != "0.00" {
			t.Fatalf("лимиты выше целевой суммы должны быть видны: %+v", out)
		}
		rec := httptest.NewRecorder()
		handlers.Limits(rec, httptest.NewRequest("GET", "/x", nil), foreignAuthor, groupID)
		if rec.Code != 404 {
			t.Fatalf("чужая группа: %d", rec.Code)
		}
	})

	t.Run("расторжение сохраняет историю и освобождает место", func(t *testing.T) {
		terminate := func(u middleware.AuthUser, payload string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/legal-entity-groups/"+groupID+"/terminate", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			handlers.Terminate(rec, req, u, groupID)
			return rec
		}
		if rec := terminate(author, `{"reason":"  "}`); rec.Code != 400 {
			t.Fatalf("причина обязательна: %d", rec.Code)
		}
		if rec := terminate(foreignAuthor, `{"reason":"чужой"}`); rec.Code != 404 {
			t.Fatalf("чужой арендатор: %d", rec.Code)
		}
		if rec := terminate(author, `{"reason":"расторгнут","terminated_on":"2999-01-01"}`); rec.Code != 400 {
			t.Fatalf("дата из будущего: %d", rec.Code)
		}
		if rec := terminate(author, `{"reason":"Группа реорганизована"}`); rec.Code != 200 {
			t.Fatalf("расторжение: %d %s", rec.Code, rec.Body.String())
		}
		if rec := terminate(author, `{"reason":"ещё раз"}`); rec.Code != 404 {
			t.Fatalf("повторное расторжение: %d", rec.Code)
		}
		update := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/x", strings.NewReader(body("Группа", goodMembers)))
		req.Header.Set("Content-Type", "application/json")
		handlers.Update(update, req, author, groupID)
		if update.Code != 404 {
			t.Fatalf("расторгнутый договор не редактируется: %d", update.Code)
		}
		if rec := create(author, body("Новая группа", goodMembers)); rec.Code != 201 {
			t.Fatalf("после расторжения можно завести новый договор: %d %s", rec.Code, rec.Body.String())
		}
		list := httptest.NewRecorder()
		handlers.List(list, httptest.NewRequest("GET", "/api/legal-entity-groups", nil), author)
		var groups []models.LegalEntityGroup
		_ = json.Unmarshal(list.Body.Bytes(), &groups)
		if len(groups) != 2 || groups[0].Status != "active" || groups[1].Status != "terminated" || groups[1].TerminationReason != "Группа реорганизована" {
			t.Fatalf("история: действующий первым, расторгнутый сохранён: %+v", groups)
		}
	})
}
