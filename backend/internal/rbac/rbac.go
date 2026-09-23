// Package rbac описывает модель разрешений ТЗ 4.4 данными: восемь ролей, их
// полномочия и область видимости лежат одной таблицей, а не рассыпаны по
// условиям в обработчиках. Роль без описания здесь не получает ничего, поэтому
// добавить роль и забыть про её права невозможно.
package rbac

import "cybercalc/internal/models"

type Permission string

const (
	// Подготовка плана и отчётности — обязанность ИТ-организации (Приказ № 270).
	PrepareReports Permission = "reports.prepare"
	// Утверждение отчёта — отдельное полномочие: готовит один, утверждает другой.
	ApproveReports Permission = "reports.approve"
	// Правка и создание мероприятий любого вида.
	EditAnyEntry   Permission = "entries.edit_any"
	CreateAnyEntry Permission = "entries.create_any"
	// Профильные полномочия специалистов: только свой предмет.
	EditInternshipEntries Permission = "entries.edit_internship"
	EditTeacherEntries    Permission = "entries.edit_teachers"
	// Справочник образовательных организаций.
	ProposeEducationDirectory Permission = "directory.propose"
	ApproveEducationDirectory Permission = "directory.approve"
	ManagePartnerStructure    Permission = "partner.manage_structure"
	// Закрепление кураторов за партнёрами (DATA-09): кто закрепляет — тот не
	// куратор, иначе куратор расширял бы собственную область видимости.
	ManageCuratorAssignments Permission = "curators.manage_assignments"
	// Постановка и переназначение задач workflow (SEC-10). Исполнять задачу
	// может её исполнитель; назначать и перенаправлять — только администрация.
	DispatchTasks Permission = "tasks.dispatch"
	// Справочник аккредитованных ИТ-компаний.
	ManageITCompanies Permission = "it_companies.manage"
	// Неизменяемые снимки на 1 мая.
	SealSnapshot     Permission = "snapshot.seal"
	DownloadSnapshot Permission = "snapshot.download"
	// Чтение данных своего арендатора.
	ReadTenantData Permission = "tenant.read"
)

// Scope — граница видимости роли. Она не выводится из полномочий: куратор и
// администратор могут иметь одно и то же полномочие при разной области.
type Scope string

const (
	// ScopeTenant — все данные своей ИТ-организации.
	ScopeTenant Scope = "tenant"
	// ScopePartner — только закреплённая образовательная организация.
	ScopePartner Scope = "partner"
)

// Definition — строка модели разрешений. Delegated описывает полномочия,
// которые роль получает как исполнитель бесхозной профильной задачи: они
// ограничены тем же предметом и не расширяют область видимости.
type Definition struct {
	Title       string
	Scope       Scope
	Permissions []Permission
	Delegated   []Permission
}

// Roles — единственное описание модели разрешений. Порядок ролей повторяет ТЗ.
var Roles = map[models.Role]Definition{
	models.RoleSuperAdmin: {
		Title: "Системный администратор", Scope: ScopeTenant,
		Permissions: []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
			EditInternshipEntries, EditTeacherEntries, ApproveEducationDirectory,
			ManagePartnerStructure, ManageCuratorAssignments, DispatchTasks, ManageITCompanies, SealSnapshot, DownloadSnapshot, ReadTenantData},
	},
	models.RoleHoldingAdmin: {
		Title: "Администратор холдинга", Scope: ScopeTenant,
		Permissions: []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
			EditInternshipEntries, EditTeacherEntries, ManagePartnerStructure, ManageCuratorAssignments, DispatchTasks, ManageITCompanies,
			SealSnapshot, DownloadSnapshot, ReadTenantData},
	},
	models.RoleOrgAdmin: {
		Title: "Администратор организации", Scope: ScopeTenant,
		Permissions: []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
			EditInternshipEntries, EditTeacherEntries, ProposeEducationDirectory,
			ManagePartnerStructure, ManageCuratorAssignments, DispatchTasks, SealSnapshot, DownloadSnapshot, ReadTenantData},
	},
	models.RoleCurator: {
		Title: "Куратор направления", Scope: ScopePartner,
		Permissions: []Permission{PrepareReports, EditAnyEntry, CreateAnyEntry,
			EditInternshipEntries, EditTeacherEntries, ManagePartnerStructure, ReadTenantData},
		// Бесхозная профильная задача достаётся куратору, но не даёт ему прав
		// специалиста сверх предмета этой задачи.
		Delegated: []Permission{EditInternshipEntries, EditTeacherEntries},
	},
	models.RoleHRSpecialist: {
		Title: "Кадровая служба", Scope: ScopeTenant,
		Permissions: []Permission{EditAnyEntry, CreateAnyEntry, EditInternshipEntries, ReadTenantData},
	},
	models.RoleFinancialSpecialist: {
		Title: "Финансовая служба", Scope: ScopeTenant,
		Permissions: []Permission{EditAnyEntry, EditTeacherEntries, ReadTenantData},
	},
	models.RoleLegalSpecialist: {
		// ТЗ §7.5: доступ на чтение реквизитов без права изменения сумм.
		Title: "Юридическое управление", Scope: ScopeTenant,
		Permissions: []Permission{ReadTenantData},
	},
	models.RoleAuditorViewer: {
		Title: "Аудитор", Scope: ScopeTenant,
		Permissions: []Permission{DownloadSnapshot, ReadTenantData},
	},
}

// Permissions перечисляет все объявленные полномочия.
func Permissions() []Permission {
	return []Permission{PrepareReports, ApproveReports, EditAnyEntry, CreateAnyEntry,
		EditInternshipEntries, EditTeacherEntries, ProposeEducationDirectory,
		ApproveEducationDirectory, ManagePartnerStructure, ManageCuratorAssignments, DispatchTasks, ManageITCompanies,
		SealSnapshot, DownloadSnapshot, ReadTenantData}
}

// Known сообщает, объявлено ли полномочие: задача не может требовать того, чего
// в модели нет.
func Known(permission Permission) bool {
	for _, declared := range Permissions() {
		if declared == permission {
			return true
		}
	}
	return false
}

// All перечисляет роли модели в порядке ТЗ.
func All() []models.Role {
	return []models.Role{
		models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator,
		models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist,
		models.RoleAuditorViewer,
	}
}

// Allows отвечает по таблице: неизвестная роль не получает ничего.
func Allows(role models.Role, permission Permission) bool {
	definition, known := Roles[role]
	if !known {
		return false
	}
	for _, granted := range definition.Permissions {
		if granted == permission {
			return true
		}
	}
	return false
}

// Delegates отвечает, может ли роль принять бесхозную профильную задачу.
// Делегирование не добавляет полномочий сверх перечисленных.
func Delegates(role models.Role, permission Permission) bool {
	definition, known := Roles[role]
	if !known {
		return false
	}
	for _, granted := range definition.Delegated {
		if granted == permission {
			return true
		}
	}
	return false
}

// ScopeOf возвращает область видимости роли. Профиль образовательной
// организации всегда ограничен своей организацией, какую бы роль ни имел.
func ScopeOf(role models.Role, entityType models.EntityType) Scope {
	if entityType == models.EntityEduInst {
		return ScopePartner
	}
	definition, known := Roles[role]
	if !known {
		return ScopePartner
	}
	return definition.Scope
}
