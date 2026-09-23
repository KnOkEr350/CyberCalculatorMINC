package handlers

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/testfixtures"
)

// SEC-09: матрица «роль × вид мероприятия × действие» на настоящих обработчиках.
// Ожидания записаны здесь независимо от access.go — по ТЗ §7.5: авторская
// сторона (администраторы и куратор) работает со всеми видами, кадровая служба
// — только со стажировками и практикой, финансовая — правит выплаты
// преподавателей, юрист и аудитор ничего не меняют, а профиль образовательной
// организации — контрагент и не автор.
func TestRoleByCategoryMatrix(t *testing.T) {
	db, year := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tn := newTenant(ctx, t, f)
	handlers := EntryHandlers{DB: db}

	categories := []string{"teachers", "ood_rpd", "internship", "employment_practice", "top_it", "it_clubs", "teacher_training", "edu_content"}
	entries := map[string]string{}
	for _, category := range categories {
		audience := "vuz"
		if category == "it_clubs" || category == "teacher_training" || category == "edu_content" {
			audience = "school"
		}
		var id string
		if err := db.QueryRowContext(ctx, `INSERT INTO entries(category_code,partner_id,agreement_id,it_company_id,period_type,report_year,
			audience,payload,amount_rub,formula_amount_rub,cost_method,created_by)
			VALUES($1,$2,$3,$4,'fact',$5,$6,'{}',100000,100000,'average',$7) RETURNING id::text`,
			category, tn.partner, tn.agreement, tn.company, year, audience, tn.admin).Scan(&id); err != nil {
			t.Fatalf("%s: %v", category, err)
		}
		entries[category] = id
	}

	type actor struct {
		name string
		user middleware.AuthUser
	}
	org := func(role models.Role) middleware.AuthUser {
		return middleware.AuthUser{ID: tn.admin, Role: role, EntityType: models.EntityOrganization, ITCompanyID: &tn.company}
	}
	actors := []actor{
		{"super_admin", org(models.RoleSuperAdmin)}, {"holding_admin", org(models.RoleHoldingAdmin)}, {"org_admin", org(models.RoleOrgAdmin)},
		{"curator", middleware.AuthUser{ID: tn.admin, Role: models.RoleCurator, EntityType: models.EntityOrganization, ITCompanyID: &tn.company, PartnerID: &tn.partner}},
		{"hr_specialist", org(models.RoleHRSpecialist)}, {"financial_specialist", org(models.RoleFinancialSpecialist)},
		{"legal_specialist", org(models.RoleLegalSpecialist)}, {"auditor_viewer", org(models.RoleAuditorViewer)},
		{"edu_curator", middleware.AuthUser{ID: tn.admin, Role: models.RoleCurator, EntityType: models.EntityEduInst, PartnerID: &tn.partner}},
	}
	// allowed[роль] — виды, к которым роль допускается: create, update.
	all := map[string]bool{}
	for _, c := range categories {
		all[c] = true
	}
	practice := map[string]bool{"internship": true, "employment_practice": true}
	none := map[string]bool{}
	allowedCreate := map[string]map[string]bool{
		"super_admin": all, "holding_admin": all, "org_admin": all, "curator": all,
		"hr_specialist": practice, "financial_specialist": none, "legal_specialist": none, "auditor_viewer": none, "edu_curator": none,
	}
	allowedUpdate := map[string]map[string]bool{
		"super_admin": all, "holding_admin": all, "org_admin": all, "curator": all,
		"hr_specialist": practice, "financial_specialist": {"teachers": true}, "legal_specialist": none, "auditor_viewer": none, "edu_curator": none,
	}

	for _, a := range actors {
		for _, category := range categories {
			// Создание: до проверки полей запрос останавливается на роли.
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/entries", strings.NewReader(fmt.Sprintf(`{"category_code":%q}`, category)))
			req.Header.Set("Content-Type", "application/json")
			handlers.Create(rec, req, a.user)
			if denied, want := rec.Code == 403, !allowedCreate[a.name][category]; denied != want {
				t.Errorf("создание %s ролью %s: код %d, отказ ожидался = %v (%s)", category, a.name, rec.Code, want, strings.TrimSpace(rec.Body.String()))
			}

			// Изменение: комментарий есть, поэтому дальше роли остаётся только
			// содержательная проверка (400). Финансист по своему Виду 1 доходит
			// до проверки полей выплаты: отказ там — не отказ по виду.
			rec = httptest.NewRecorder()
			req = httptest.NewRequest("PUT", "/api/entries/"+entries[category], strings.NewReader(`{"comment":"проверка доступа","payload":{}}`))
			req.Header.Set("Content-Type", "application/json")
			handlers.Update(rec, req, a.user, entries[category])
			if a.name == "financial_specialist" && category == "teachers" {
				if !strings.Contains(rec.Body.String(), "компенсационной выплаты") && rec.Code == 403 {
					t.Errorf("финансист по Виду 1: отказ должен объясняться полями выплаты, а не видом: %s", rec.Body.String())
				}
				continue
			}
			if denied, want := rec.Code == 403, !allowedUpdate[a.name][category]; denied != want {
				t.Errorf("изменение %s ролью %s: код %d, отказ ожидался = %v (%s)", category, a.name, rec.Code, want, strings.TrimSpace(rec.Body.String()))
			}
		}
	}
}
