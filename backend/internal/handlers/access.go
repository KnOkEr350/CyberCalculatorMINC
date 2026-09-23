package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"database/sql"
	"net/http"
)

func itCompanyScope(u middleware.AuthUser) string {
	if u.ITCompanyID != nil {
		return *u.ITCompanyID
	}
	// An organization profile is a tenant, not an operator. If its assignment
	// is incomplete, return a scope that cannot match a UUID instead of
	// accidentally falling back to operator-wide access.
	if u.EntityType == models.EntityOrganization {
		return "unassigned"
	}
	return ""
}

func requireITCompanyForWrite(w http.ResponseWriter, u middleware.AuthUser) (string, bool) {
	if u.ITCompanyID == nil || *u.ITCompanyID == "" {
		middleware.WriteError(w, http.StatusForbidden, "для операции назначьте профилю действующую ИТ-компанию")
		return "", false
	}
	return *u.ITCompanyID, true
}

// An organization profile represents the obligated IT organization and works
// across its educational partners. Admin and moderator are operator roles.
func isStaff(u middleware.AuthUser) bool {
	if u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin {
		return true
	}
	return u.EntityType == models.EntityOrganization && (u.Role == models.RoleOrgAdmin || u.Role == models.RoleCurator)
}

func canReadTenantData(u middleware.AuthUser) bool {
	return isStaff(u) || (u.EntityType == models.EntityOrganization && models.ValidRole(u.Role))
}

// The Order assigns preparation of the plan and reports to the obligated IT
// organization. An educational organization is a counterparty: it may read its
// own materials and review a submitted fact report, but it must not author it.
func canPrepareReports(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityEduInst {
		return false
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator:
		return true
	default:
		return false
	}
}

func canEditEntryCategory(u middleware.AuthUser, category string) bool {
	if canPrepareReports(u) {
		return true
	}
	// Профиль образовательной организации — контрагент, а не автор: он не
	// правит мероприятия ИТ-организации ни в одной роли. Создание это уже
	// учитывало (canCreateEntryCategory), обновление — нет, поэтому профильный
	// специалист со стороны ОО мог изменить чужую строку.
	if u.EntityType == models.EntityEduInst {
		return false
	}
	switch u.Role {
	case models.RoleHRSpecialist:
		return category == "internship" || category == "employment_practice"
	case models.RoleFinancialSpecialist:
		return category == "teachers"
	default:
		return false
	}
}

func canEditAnyEntry(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityEduInst {
		return false
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator,
		models.RoleHRSpecialist, models.RoleFinancialSpecialist:
		return true
	default:
		return false
	}
}

func canCreateEntryCategory(u middleware.AuthUser, category string) bool {
	if canPrepareReports(u) {
		return true
	}
	return u.EntityType != models.EntityEduInst && u.Role == models.RoleHRSpecialist &&
		(category == "internship" || category == "employment_practice")
}

func canCreateAnyEntry(u middleware.AuthUser) bool {
	return canPrepareReports(u) || (u.EntityType != models.EntityEduInst && u.Role == models.RoleHRSpecialist)
}

func canUploadDocument(u middleware.AuthUser, category, documentType string) bool {
	if u.EntityType == models.EntityEduInst {
		return u.Role == models.RoleCurator && documentType == "outgoing_certificate"
	}
	if u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin || u.Role == models.RoleOrgAdmin {
		return true
	}
	if (category == "internship" || category == "employment_practice") &&
		(documentType == "labor_contract" || documentType == "incoming_certificate" || documentType == "mentor_order") {
		return u.Role == models.RoleHRSpecialist
	}
	if documentType == "outgoing_certificate" {
		return u.Role == models.RoleCurator
	}
	if category == "teachers" && documentType == "payment_order" {
		return u.Role == models.RoleFinancialSpecialist
	}
	return u.Role == models.RoleCurator
}

func canUploadAnyDocument(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityEduInst {
		return u.Role == models.RoleCurator
	}
	switch u.Role {
	case models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator, models.RoleHRSpecialist, models.RoleFinancialSpecialist:
		return true
	default:
		return false
	}
}

func isEducationRepresentative(u middleware.AuthUser) bool {
	return u.EntityType == models.EntityEduInst && u.PartnerID != nil && *u.PartnerID != ""
}

func canReviewReport(u middleware.AuthUser, period string) bool {
	if period != string(models.PeriodFact) {
		return canPrepareReports(u)
	}
	// Administrators and moderators may record deemed approval after the
	// statutory response period. A regular IT-organization user cannot approve
	// the counterparty's own review.
	return isEducationRepresentative(u) ||
		(u.EntityType != models.EntityEduInst && (u.Role == models.RoleSuperAdmin || u.Role == models.RoleOrgAdmin || u.Role == models.RoleLegalSpecialist))
}

func canApproveReports(u middleware.AuthUser) bool {
	return u.EntityType != models.EntityEduInst && (u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin || u.Role == models.RoleOrgAdmin)
}

func canManageITCompanies(u middleware.AuthUser) bool {
	return u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin
}

func canViewITCompanies(u middleware.AuthUser) bool {
	return models.ValidRole(u.Role)
}

// Administrators and moderators work with the counterparty directory:
// IT-organization profiles review educational organizations, while educational-
// organization profiles review accredited IT companies. The user-role branches
// preserve the existing non-administrative workspaces.
func canReviewEducationDirectory(u middleware.AuthUser) bool {
	if u.EntityType == models.EntityOrganization {
		return true
	}
	return u.Role == models.RoleCurator && u.EntityType == models.EntityEduInst
}

func canManagePartnerStructure(u middleware.AuthUser) bool {
	if u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin {
		return true
	}
	if u.EntityType == models.EntityOrganization {
		return u.Role == models.RoleOrgAdmin || u.Role == models.RoleCurator
	}
	return u.EntityType == models.EntityEduInst && u.Role == models.RoleCurator
}

func canProposeEducationDirectory(u middleware.AuthUser) bool {
	return u.Role == models.RoleOrgAdmin && u.EntityType == models.EntityOrganization
}

func canApproveEducationDirectory(u middleware.AuthUser) bool {
	return u.Role == models.RoleSuperAdmin && u.EntityType == models.EntityOrganization
}
func canAccessPartner(u middleware.AuthUser, id string) bool {
	if u.EntityType == models.EntityEduInst {
		return u.PartnerID != nil && *u.PartnerID == id && id != ""
	}
	if u.EntityType == models.EntityOrganization && u.Role == models.RoleCurator {
		return u.PartnerID != nil && *u.PartnerID == id && id != ""
	}
	return canReadTenantData(u)
}
func requirePartner(w http.ResponseWriter, u middleware.AuthUser, id string) bool {
	if !canAccessPartner(u, id) {
		middleware.WriteError(w, 403, "нет доступа к выбранному учебному заведению")
		return false
	}
	return true
}

// requirePartnerTenant adds tenant ownership to the role/partner check. An
// unassigned administrator may inspect all tenants, but organization users can
// never cross their assigned IT-company boundary.
func requirePartnerTenant(w http.ResponseWriter, r *http.Request, db *sql.DB, u middleware.AuthUser, id string) bool {
	if !requirePartner(w, u, id) {
		return false
	}
	if u.EntityType == models.EntityEduInst || (u.EntityType == "" && u.ITCompanyID == nil && (u.Role == models.RoleSuperAdmin || u.Role == models.RoleHoldingAdmin)) {
		return true
	}
	if u.ITCompanyID == nil {
		middleware.WriteError(w, http.StatusForbidden, "профилю не назначена ИТ-компания")
		return false
	}
	var allowed bool
	if err := db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM partners WHERE id::text=$1 AND it_company_id::text=$2)`, id, *u.ITCompanyID).Scan(&allowed); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "не удалось проверить владельца учебного заведения")
		return false
	}
	if !allowed {
		middleware.WriteError(w, http.StatusForbidden, "учебное заведение относится к другой ИТ-компании")
	}
	return allowed
}
func requireEntry(w http.ResponseWriter, r *http.Request, db *sql.DB, u middleware.AuthUser, id string) bool {
	var partner sql.NullString
	err := db.QueryRowContext(r.Context(), `SELECT partner_id FROM entries WHERE id::text=$1`, id).Scan(&partner)
	if err == sql.ErrNoRows {
		middleware.WriteError(w, 404, "запись не найдена")
		return false
	}
	if err != nil {
		middleware.WriteError(w, 500, "не удалось проверить доступ")
		return false
	}
	return requirePartnerTenant(w, r, db, u, partner.String)
}
func partnerScope(u middleware.AuthUser, requested string) string {
	if u.EntityType == models.EntityEduInst && u.PartnerID != nil {
		return *u.PartnerID
	}
	if u.EntityType == models.EntityEduInst {
		return "unassigned"
	}
	if u.EntityType == models.EntityOrganization && u.Role == models.RoleCurator {
		if u.PartnerID != nil && *u.PartnerID != "" {
			return *u.PartnerID
		}
		return "unassigned"
	}
	if canReadTenantData(u) {
		return requested
	}
	return "unassigned"
}
