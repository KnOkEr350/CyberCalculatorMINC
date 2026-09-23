package rbac

import (
	"testing"

	"cybercalc/internal/models"
)

// SEC-01: модель разрешений описана данными. Роль, добавленная в модель и не
// внесённая в таблицу, не получает ничего — и это должно быть видно сразу, а
// не проявиться отсутствующей проверкой в одном из обработчиков.
func TestEveryModelRoleIsDescribed(t *testing.T) {
	for _, role := range All() {
		if !models.ValidRole(role) {
			t.Fatalf("роль %s описана, но неизвестна модели", role)
		}
		definition, described := Roles[role]
		if !described {
			t.Fatalf("роль %s не описана в модели разрешений", role)
		}
		if definition.Title == "" {
			t.Errorf("роль %s без названия: таблицу нельзя читать как документ", role)
		}
		if definition.Scope != ScopeTenant && definition.Scope != ScopePartner {
			t.Errorf("роль %s без области видимости", role)
		}
		if len(definition.Permissions) == 0 {
			t.Errorf("роль %s без единого полномочия: такую роль незачем заводить", role)
		}
	}
	if len(Roles) != len(All()) {
		t.Fatalf("в таблице %d ролей, в перечне %d", len(Roles), len(All()))
	}
}

// Неизвестная роль не получает ничего: подстановка произвольного значения в
// профиль не должна открывать доступ.
func TestUnknownRoleGetsNothing(t *testing.T) {
	for _, role := range []models.Role{"", "root", "system", "admin", "superuser"} {
		if models.ValidRole(role) {
			t.Fatalf("роль %q не должна проходить проверку модели", role)
		}
		for _, permission := range everyPermission() {
			if Allows(role, permission) {
				t.Errorf("роль %q получила полномочие %s", role, permission)
			}
			if Delegates(role, permission) {
				t.Errorf("роль %q получила делегированное полномочие %s", role, permission)
			}
		}
		if ScopeOf(role, models.EntityOrganization) != ScopePartner {
			t.Errorf("неизвестная роль %q должна получать самую узкую область", role)
		}
	}
}

// QA-02: делегирование бесхозной задачи не расширяет полномочия. Куратор
// принимает профильную задачу, но не становится специалистом сверх того, что
// у него уже есть.
func TestDelegationNeverWidensPermissions(t *testing.T) {
	for role, definition := range Roles {
		for _, delegated := range definition.Delegated {
			if !Allows(role, delegated) {
				t.Errorf("%s получает через делегирование полномочие %s, которого нет в её правах",
					role, delegated)
			}
		}
	}
}

// Вертикальный обход: профильный специалист не поднимается до подготовки и
// утверждения отчётности, а утверждение отделено от подготовки.
func TestSpecialistsDoNotReachReportingAuthority(t *testing.T) {
	for _, role := range []models.Role{models.RoleHRSpecialist, models.RoleFinancialSpecialist,
		models.RoleLegalSpecialist, models.RoleAuditorViewer} {
		for _, permission := range []Permission{PrepareReports, ApproveReports, SealSnapshot,
			ApproveEducationDirectory, ManageITCompanies} {
			if Allows(role, permission) {
				t.Errorf("%s не может получать полномочие %s", role, permission)
			}
		}
	}
	// Куратор готовит отчёт, но не утверждает его сам.
	if !Allows(models.RoleCurator, PrepareReports) {
		t.Error("куратор готовит отчётность")
	}
	if Allows(models.RoleCurator, ApproveReports) {
		t.Error("куратор не может утверждать собственный отчёт")
	}
	// Утверждение справочника — только у системного администратора.
	for _, role := range All() {
		if role == models.RoleSuperAdmin {
			continue
		}
		if Allows(role, ApproveEducationDirectory) {
			t.Errorf("%s не может утверждать записи справочника ОО", role)
		}
	}
}

// Горизонтальный обход: профиль образовательной организации ограничен своей
// организацией в любой роли, а куратор ИТ-компании — своим закреплением.
func TestScopeNeverWidensForEducationProfile(t *testing.T) {
	for _, role := range All() {
		if got := ScopeOf(role, models.EntityEduInst); got != ScopePartner {
			t.Errorf("%s в профиле ОО получила область %s", role, got)
		}
	}
	if ScopeOf(models.RoleCurator, models.EntityOrganization) != ScopePartner {
		t.Error("куратор видит только закреплённую образовательную организацию")
	}
	for _, role := range []models.Role{models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin} {
		if ScopeOf(role, models.EntityOrganization) != ScopeTenant {
			t.Errorf("%s работает по всей своей ИТ-организации", role)
		}
	}
}

// Юрист по ТЗ §7.5 имеет доступ на чтение и не меняет ни исходные, ни
// финансовые данные: иначе проверяющий правил бы то, что проверяет.
func TestLegalSpecialistIsReadOnly(t *testing.T) {
	definition := Roles[models.RoleLegalSpecialist]
	if len(definition.Permissions) != 1 || definition.Permissions[0] != ReadTenantData {
		t.Fatalf("юрист должен иметь ровно одно полномочие — чтение: %v", definition.Permissions)
	}
	for _, permission := range everyPermission() {
		if permission == ReadTenantData {
			continue
		}
		if Allows(models.RoleLegalSpecialist, permission) {
			t.Errorf("юрист получил полномочие %s", permission)
		}
	}
}

// Аудитор смотрит и выгружает, но ничего не меняет: иначе проверяющий мог бы
// править то, что проверяет.
func TestAuditorIsStrictlyReadOnly(t *testing.T) {
	writing := []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
		EditInternshipEntries, EditTeacherEntries, ProposeEducationDirectory,
		ApproveEducationDirectory, ManagePartnerStructure, ManageITCompanies, SealSnapshot}
	for _, permission := range writing {
		if Allows(models.RoleAuditorViewer, permission) {
			t.Errorf("аудитор получил изменяющее полномочие %s", permission)
		}
	}
	for _, permission := range []Permission{ReadTenantData, DownloadSnapshot} {
		if !Allows(models.RoleAuditorViewer, permission) {
			t.Errorf("аудитор должен иметь полномочие %s", permission)
		}
	}
}

func everyPermission() []Permission {
	return []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
		EditInternshipEntries, EditTeacherEntries, ProposeEducationDirectory,
		ApproveEducationDirectory, ManagePartnerStructure, ManageITCompanies,
		SealSnapshot, DownloadSnapshot, ReadTenantData}
}

// Каждое объявленное полномочие кому-то принадлежит: неиспользуемое
// полномочие — признак, что проверка потерялась.
func TestEveryPermissionIsGrantedToSomeone(t *testing.T) {
	for _, permission := range everyPermission() {
		granted := false
		for _, role := range All() {
			if Allows(role, permission) {
				granted = true
				break
			}
		}
		if !granted {
			t.Errorf("полномочие %s не принадлежит ни одной роли", permission)
		}
	}
}
