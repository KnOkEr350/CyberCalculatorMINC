package handlers

import (
	"net/http/httptest"
	"testing"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

// SEC-09: матрица «роль × операция × арендатор» одним столом решений. Отдельные
// тесты проверяют отдельные случаи, и новая роль тихо проходит между ними: ни
// один из них не обязан о ней знать. Здесь роль без строки в матрице — ошибка.
type rolePermissions struct {
	staff            bool // операторские права внутри ИТ-организации
	readTenant       bool // чтение данных своего арендатора
	prepareReports   bool // подготовка плана и отчёта
	editAnyEntry     bool // правка любой строки мероприятий
	editInternship   bool // правка стажировок и практики
	editTeachers     bool // правка Вида 1
	approveDirectory bool // утверждение записи в справочнике ОО
	proposeDirectory bool // предложение записи в справочник ОО
	manageStructure  bool // подразделения и академические группы партнёра
}

var rbacMatrix = map[models.Role]rolePermissions{
	models.RoleSuperAdmin: {staff: true, readTenant: true, prepareReports: true, editAnyEntry: true,
		editInternship: true, editTeachers: true, approveDirectory: true, proposeDirectory: false, manageStructure: true},
	models.RoleHoldingAdmin: {staff: true, readTenant: true, prepareReports: true, editAnyEntry: true,
		editInternship: true, editTeachers: true, approveDirectory: false, proposeDirectory: false, manageStructure: true},
	models.RoleOrgAdmin: {staff: true, readTenant: true, prepareReports: true, editAnyEntry: true,
		editInternship: true, editTeachers: true, approveDirectory: false, proposeDirectory: true, manageStructure: true},
	models.RoleCurator: {staff: true, readTenant: true, prepareReports: true, editAnyEntry: true,
		editInternship: true, editTeachers: true, approveDirectory: false, proposeDirectory: false, manageStructure: true},
	// Профильные специалисты правят только свой предмет и не готовят отчётность.
	models.RoleHRSpecialist: {staff: false, readTenant: true, prepareReports: false, editAnyEntry: true,
		editInternship: true, editTeachers: false, approveDirectory: false, proposeDirectory: false, manageStructure: false},
	models.RoleFinancialSpecialist: {staff: false, readTenant: true, prepareReports: false, editAnyEntry: true,
		editInternship: false, editTeachers: true, approveDirectory: false, proposeDirectory: false, manageStructure: false},
	// Юрист ведёт сомнения, но не меняет исходные и финансовые поля (ТЗ §7.5).
	models.RoleLegalSpecialist: {staff: false, readTenant: true, prepareReports: false, editAnyEntry: false,
		editInternship: false, editTeachers: false, approveDirectory: false, proposeDirectory: false, manageStructure: false},
	// Аудитор только смотрит.
	models.RoleAuditorViewer: {staff: false, readTenant: true, prepareReports: false, editAnyEntry: false,
		editInternship: false, editTeachers: false, approveDirectory: false, proposeDirectory: false, manageStructure: false},
}

func allRoles() []models.Role {
	return []models.Role{
		models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator,
		models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist,
		models.RoleAuditorViewer,
	}
}

// Роль, добавленная в модель и не внесённая в матрицу, остаётся без решения о
// правах — это и должно ломать сборку.
func TestRBACMatrixCoversEveryKnownRole(t *testing.T) {
	for _, role := range allRoles() {
		if !models.ValidRole(role) {
			t.Fatalf("роль %s перечислена в матрице, но неизвестна модели", role)
		}
		if _, described := rbacMatrix[role]; !described {
			t.Fatalf("роль %s не описана в матрице разрешений", role)
		}
	}
	if len(rbacMatrix) != len(allRoles()) {
		t.Fatalf("в матрице %d ролей, в модели %d", len(rbacMatrix), len(allRoles()))
	}
}

func TestRBACMatrixMatchesImplementation(t *testing.T) {
	company := "11111111-1111-1111-1111-111111111111"
	for role, want := range rbacMatrix {
		t.Run(string(role), func(t *testing.T) {
			user := middleware.AuthUser{ID: "user", Role: role,
				EntityType: models.EntityOrganization, ITCompanyID: &company}
			checks := []struct {
				name string
				got  bool
				want bool
			}{
				{"isStaff", isStaff(user), want.staff},
				{"canReadTenantData", canReadTenantData(user), want.readTenant},
				{"canPrepareReports", canPrepareReports(user), want.prepareReports},
				{"canEditAnyEntry", canEditAnyEntry(user), want.editAnyEntry},
				{"canEditEntryCategory(internship)", canEditEntryCategory(user, "internship"), want.editInternship},
				{"canEditEntryCategory(employment_practice)", canEditEntryCategory(user, "employment_practice"), want.editInternship},
				{"canEditEntryCategory(teachers)", canEditEntryCategory(user, "teachers"), want.editTeachers},
				{"canApproveEducationDirectory", canApproveEducationDirectory(user), want.approveDirectory},
				{"canProposeEducationDirectory", canProposeEducationDirectory(user), want.proposeDirectory},
				{"canManagePartnerStructure", canManagePartnerStructure(user), want.manageStructure},
			}
			for _, check := range checks {
				if check.got != check.want {
					t.Errorf("%s: получено %v, ожидалось %v", check.name, check.got, check.want)
				}
			}
		})
	}
}

// Арендатор — вторая ось матрицы: профиль образовательной организации не
// готовит отчётность ни в одной роли, потому что по Приказу это обязанность
// ИТ-организации.
func TestEducationProfileNeverAuthorsReports(t *testing.T) {
	partner := "22222222-2222-2222-2222-222222222222"
	for _, role := range allRoles() {
		user := middleware.AuthUser{ID: "user", Role: role,
			EntityType: models.EntityEduInst, PartnerID: &partner}
		if canPrepareReports(user) {
			t.Errorf("%s: профиль ОО не может готовить отчётность", role)
		}
		if canEditAnyEntry(user) {
			t.Errorf("%s: профиль ОО не может править мероприятия", role)
		}
		for _, category := range []string{"teachers", "internship", "employment_practice", "top_it"} {
			if canEditEntryCategory(user, category) {
				t.Errorf("%s: профиль ОО не может править вид %s", role, category)
			}
		}
	}
}

// Незаполненный арендатор не должен превращаться в доступ ко всем данным:
// область поиска обязана оставаться несопоставимой ни с одним UUID.
func TestUnassignedTenantDoesNotWiden(t *testing.T) {
	for _, role := range allRoles() {
		user := middleware.AuthUser{ID: "user", Role: role, EntityType: models.EntityOrganization}
		if scope := itCompanyScope(user); scope != "unassigned" {
			t.Errorf("%s: область незакреплённого профиля %q вместо \"unassigned\"", role, scope)
		}
		if _, ok := requireITCompanyForWrite(httptest.NewRecorder(), user); ok {
			t.Errorf("%s: запись без назначенной ИТ-компании должна отклоняться", role)
		}
	}
}
