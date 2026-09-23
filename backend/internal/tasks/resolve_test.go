package tasks

import (
	"strings"
	"testing"

	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
)

func TestSpecialistComesFirst(t *testing.T) {
	d := Resolve(Requirement{Role: models.RoleHRSpecialist, Permission: rbac.EditInternshipEntries, HasPartner: true},
		Candidates{Specialists: []string{"hr-1", "hr-2"}, Curators: []string{"cur"}, OrgAdmins: []string{"oa"}, SuperAdmins: []string{"sa"}})
	if d.Route != RouteSpecialist || d.Assignee != "hr-1" || d.Escalated || d.Reason != "" {
		t.Fatalf("профильный специалист должен получить задачу без fallback: %+v", d)
	}
}

func TestFallbackChainFollowsADR22(t *testing.T) {
	req := Requirement{Role: models.RoleHRSpecialist, Permission: rbac.EditInternshipEntries, HasPartner: true}
	cases := []struct {
		name  string
		c     Candidates
		route Route
		who   string
		esc   bool
	}{
		{"куратор при отсутствии специалиста", Candidates{Curators: []string{"cur"}, OrgAdmins: []string{"oa"}, SuperAdmins: []string{"sa"}}, RouteCurator, "cur", false},
		{"ORG_ADMIN при отсутствии куратора", Candidates{OrgAdmins: []string{"oa"}, SuperAdmins: []string{"sa"}}, RouteOrgAdmin, "oa", true},
		{"SUPER_ADMIN при отсутствии ORG_ADMIN", Candidates{SuperAdmins: []string{"sa"}}, RouteSuperAdmin, "sa", true},
		{"никого", Candidates{}, RouteUnassigned, "", true},
	}
	for _, tc := range cases {
		d := Resolve(req, tc.c)
		if d.Route != tc.route || d.Assignee != tc.who || d.Escalated != tc.esc {
			t.Errorf("%s: %+v", tc.name, d)
		}
		if d.Reason == "" {
			t.Errorf("%s: причина fallback обязательна", tc.name)
		}
	}
}

// Fallback не выдаёт куратору права специалиста: если действие ему не
// делегируется, задача уходит выше, даже когда куратор есть.
func TestCuratorDoesNotReceiveUndelegatedWork(t *testing.T) {
	d := Resolve(Requirement{Role: models.RoleLegalSpecialist, Permission: rbac.ReadTenantData, HasPartner: true},
		Candidates{Curators: []string{"cur"}, OrgAdmins: []string{"oa"}})
	if d.Route != RouteOrgAdmin || !strings.Contains(d.Reason, "не делегируется") {
		t.Fatalf("неделегируемое действие должно уйти к ORG_ADMIN: %+v", d)
	}
}

func TestCuratorNeedsAPartner(t *testing.T) {
	d := Resolve(Requirement{Role: models.RoleHRSpecialist, Permission: rbac.EditInternshipEntries},
		Candidates{Curators: []string{"cur"}, OrgAdmins: []string{"oa"}})
	if d.Route != RouteOrgAdmin || !strings.Contains(d.Reason, "не привязана к партнёру") {
		t.Fatalf("без партнёра куратора выбрать нельзя: %+v", d)
	}
}

// Эскалация тоже не расширяет полномочий: администратор берёт задачу, только
// если его роль сама вправе выполнить действие.
func TestEscalationSkipsAdministratorsWithoutThePermission(t *testing.T) {
	// Утверждение справочника есть у SUPER_ADMIN, но нет у ORG_ADMIN.
	d := Resolve(Requirement{Role: models.RoleLegalSpecialist, Permission: rbac.ApproveEducationDirectory},
		Candidates{OrgAdmins: []string{"oa"}, SuperAdmins: []string{"sa"}})
	if d.Route != RouteSuperAdmin || d.Assignee != "sa" {
		t.Fatalf("ORG_ADMIN без полномочия должен быть пропущен: %+v", d)
	}
	// Полномочия нет ни у кого из администраторов — исполнителя нет.
	none := Resolve(Requirement{Role: models.RoleLegalSpecialist, Permission: rbac.Permission("does.not.exist")},
		Candidates{OrgAdmins: []string{"oa"}, SuperAdmins: []string{"sa"}})
	if none.Route != RouteUnassigned {
		t.Fatalf("без полномочия ни у кого задача остаётся без исполнителя: %+v", none)
	}
}

func TestCanExecute(t *testing.T) {
	cases := []struct {
		name       string
		role       models.Role
		assignee   bool
		route      Route
		permission rbac.Permission
		want       bool
	}{
		{"специалист выполняет своё", models.RoleHRSpecialist, true, RouteSpecialist, rbac.EditInternshipEntries, true},
		{"куратор — делегированное", models.RoleCurator, true, RouteCurator, rbac.EditInternshipEntries, true},
		{"куратор — недоделегированное", models.RoleCurator, true, RouteCurator, rbac.ApproveReports, false},
		{"куратор по маршруту curator не получает свои полномочия сверх делегированных", models.RoleCurator, true, RouteCurator, rbac.ManagePartnerStructure, false},
		{"не исполнитель", models.RoleOrgAdmin, false, RouteOrgAdmin, rbac.ApproveReports, false},
		{"администратор при эскалации", models.RoleOrgAdmin, true, RouteOrgAdmin, rbac.ApproveReports, true},
		{"аудитор ничего не закрывает", models.RoleAuditorViewer, true, RouteSpecialist, rbac.EditAnyEntry, false},
	}
	for _, tc := range cases {
		if got := CanExecute(tc.role, tc.assignee, tc.route, tc.permission); got != tc.want {
			t.Errorf("%s: %v, ожидалось %v", tc.name, got, tc.want)
		}
	}
}
