package handlers

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
	"cybercalc/internal/testfixtures"
)

// QA-02 на реальной БД: горизонтального обхода нет. Куратор закреплён за своей
// образовательной организацией и не дотягивается до соседней даже внутри одной
// ИТ-компании; профиль ОО не видит чужую организацию.
func TestNoHorizontalBypassBetweenPartners(t *testing.T) {
	db, _ := integrationDB(t)
	ctx := context.Background()
	f := testfixtures.New(db, t.Name())
	tenant := newTenant(ctx, t, f)

	// Вторая образовательная организация той же ИТ-компании.
	otherUniversity, err := f.CreateUniversity(ctx, testfixtures.EducationParams{})
	if err != nil {
		t.Fatal(err)
	}
	otherPartner, err := f.CreatePartner(ctx, testfixtures.ITCompany{ID: tenant.company}, otherUniversity)
	if err != nil {
		t.Fatal(err)
	}

	curator := middleware.AuthUser{ID: tenant.admin, Role: models.RoleCurator,
		EntityType: models.EntityOrganization, ITCompanyID: &tenant.company, PartnerID: &tenant.partner}
	if !canAccessPartner(curator, tenant.partner) {
		t.Fatal("куратор должен работать со своей образовательной организацией")
	}
	if canAccessPartner(curator, otherPartner.ID) {
		t.Fatal("куратор не должен дотягиваться до чужой образовательной организации")
	}

	// Проверка идёт и на уровне запроса: справочник наставников чужого
	// партнёра закрыт.
	handlers := EntryHandlers{DB: db}
	recorder := httptest.NewRecorder()
	handlers.Mentors(recorder, httptest.NewRequest("GET", fmt.Sprintf("/api/mentors?partner_id=%s", otherPartner.ID), nil), curator)
	if recorder.Code == 200 {
		t.Fatalf("куратор получил справочник чужой ОО: %s", recorder.Body.String())
	}

	// Профиль образовательной организации ограничен своей организацией.
	education := middleware.AuthUser{ID: tenant.admin, Role: models.RoleCurator,
		EntityType: models.EntityEduInst, PartnerID: &tenant.partner}
	if canAccessPartner(education, otherPartner.ID) {
		t.Fatal("профиль ОО не должен видеть чужую организацию")
	}
	if !canAccessPartner(education, tenant.partner) {
		t.Fatal("профиль ОО должен видеть свою организацию")
	}
}

// Незакреплённый профиль не превращается в доступ ко всему: область остаётся
// несопоставимой ни с одним партнёром.
func TestUnassignedProfileReachesNothing(t *testing.T) {
	for _, role := range rbac.All() {
		for _, entity := range []models.EntityType{models.EntityOrganization, models.EntityEduInst} {
			user := middleware.AuthUser{ID: "user", Role: role, EntityType: entity}
			if entity == models.EntityEduInst && canAccessPartner(user, "любой-идентификатор") {
				t.Errorf("%s: незакреплённый профиль ОО получил доступ к партнёру", role)
			}
			if entity == models.EntityEduInst && partnerScope(user, "") != "unassigned" {
				t.Errorf("%s: незакреплённый профиль ОО должен получать пустую область", role)
			}
			if entity == models.EntityOrganization && itCompanyScope(user) != "unassigned" {
				t.Errorf("%s: незакреплённый профиль организации должен получать пустую область", role)
			}
		}
	}
	// Куратор ИТ-компании без закрепления тоже не получает всё сразу.
	curator := middleware.AuthUser{ID: "user", Role: models.RoleCurator, EntityType: models.EntityOrganization}
	if partnerScope(curator, "") != "unassigned" {
		t.Fatal("куратор без закрепления не должен видеть всех партнёров")
	}
}
