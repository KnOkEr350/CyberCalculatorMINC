// Package tasks — диспетчер задач workflow и эскалаций (SEC-04, SEC-10, ADR-22).
//
// Цепочка назначения: профильный специалист → закреплённый куратор → ORG_ADMIN
// → SUPER_ADMIN. Выбор исполнителя — чистая функция над списком кандидатов
// (resolve.go), чтение кандидатов и запись — в store.go. Fallback меняет
// исполнителя, но не его полномочия: куратор получает задачу только если её
// полномочие в модели разрешений делегируется куратору.
package tasks

import (
	"cybercalc/internal/models"
	"cybercalc/internal/rbac"
)

// Route — шаг цепочки, на котором нашёлся исполнитель.
type Route string

const (
	RouteSpecialist Route = "specialist"
	RouteCurator    Route = "curator"
	RouteOrgAdmin   Route = "org_admin"
	RouteSuperAdmin Route = "super_admin"
	RouteUnassigned Route = "unassigned"
)

// Requirement — что требуется от исполнителя.
type Requirement struct {
	Role       models.Role
	Permission rbac.Permission
	// HasPartner — задача привязана к партнёру: куратора можно выбрать только
	// по закреплению за этим партнёром.
	HasPartner bool
}

// Candidates — активные пользователи, найденные на каждом шаге цепочки, в
// порядке предпочтения (первый — самый подходящий).
type Candidates struct {
	Specialists []string
	Curators    []string
	OrgAdmins   []string
	SuperAdmins []string
}

// Decision — результат выбора.
type Decision struct {
	Assignee string
	Route    Route
	// Escalated — задача ушла выше профильной роли и куратора.
	Escalated bool
	// Reason — почему не назначен профильный специалист; пусто у маршрута specialist.
	Reason string
}

// Resolve выбирает исполнителя по ADR-22. Куратор пропускается, если задача не
// привязана к партнёру или её полномочие куратору не делегируется: иначе
// fallback тихо выдал бы куратору права специалиста. Решение объяснимо: причина
// говорит, какие шаги цепочки были пройдены.
func Resolve(req Requirement, c Candidates) Decision {
	if len(c.Specialists) > 0 {
		return Decision{Assignee: c.Specialists[0], Route: RouteSpecialist}
	}
	reason := "нет активного пользователя роли " + string(req.Role)
	// Сама роль задачи — куратор или администратор: специалиста «выше» нет, шаги
	// ниже по цепочке применяются как обычно.
	switch {
	case !req.HasPartner:
		reason += "; задача не привязана к партнёру, куратора выбрать нельзя"
	case !rbac.Delegates(models.RoleCurator, req.Permission):
		reason += "; полномочие " + string(req.Permission) + " куратору не делегируется"
	case len(c.Curators) == 0:
		reason += "; за партнёром нет действующего куратора"
	default:
		return Decision{Assignee: c.Curators[0], Route: RouteCurator, Reason: reason + "; задача назначена закреплённому куратору"}
	}
	// Эскалация не выдаёт прав: администратор берёт задачу только если его роль
	// сама вправе выполнить это действие.
	if len(c.OrgAdmins) > 0 && rbac.Allows(models.RoleOrgAdmin, req.Permission) {
		return Decision{Assignee: c.OrgAdmins[0], Route: RouteOrgAdmin, Escalated: true, Reason: reason + "; эскалация ORG_ADMIN"}
	}
	if len(c.SuperAdmins) > 0 && rbac.Allows(models.RoleSuperAdmin, req.Permission) {
		return Decision{Assignee: c.SuperAdmins[0], Route: RouteSuperAdmin, Escalated: true, Reason: reason + "; эскалация SUPER_ADMIN"}
	}
	return Decision{Route: RouteUnassigned, Escalated: true, Reason: reason + "; исполнителя нет ни на одном шаге цепочки"}
}

// CanExecute — вправе ли пользователь выполнить задачу. Исполнитель выполняет
// её в пределах полномочий своей роли; по маршруту куратора — только в пределах
// делегированных. Не исполнитель задачу не закрывает.
func CanExecute(user models.Role, isAssignee bool, route Route, permission rbac.Permission) bool {
	if !isAssignee {
		return false
	}
	if route == RouteCurator {
		return rbac.Delegates(user, permission)
	}
	return rbac.Allows(user, permission)
}
