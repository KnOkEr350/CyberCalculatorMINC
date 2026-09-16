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
	return u.Role == models.RoleAdmin || u.Role == models.RoleModerator || u.EntityType == models.EntityOrganization
}

// The Order assigns preparation of the plan and reports to the obligated IT
// organization. An educational organization is a counterparty: it may read its
// own materials and review a submitted fact report, but it must not author it.
func canPrepareReports(u middleware.AuthUser) bool {
	return u.EntityType != models.EntityEduInst && isStaff(u)
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
		(u.EntityType != models.EntityEduInst && (u.Role == models.RoleAdmin || u.Role == models.RoleModerator))
}

func canApproveReports(u middleware.AuthUser) bool {
	return u.EntityType != models.EntityEduInst && (u.Role == models.RoleAdmin || u.Role == models.RoleModerator)
}

func canManageITCompanies(u middleware.AuthUser) bool {
	if u.Role == models.RoleAdmin || u.Role == models.RoleModerator {
		return u.EntityType == models.EntityEduInst
	}
	return u.EntityType == models.EntityOrganization
}

func canViewITCompanies(u middleware.AuthUser) bool {
	return canManageITCompanies(u) || (u.Role == models.RoleUser && u.EntityType == models.EntityEduInst)
}

// Administrators and moderators work with the counterparty directory:
// IT-organization profiles review educational organizations, while educational-
// organization profiles review accredited IT companies. The user-role branches
// preserve the existing non-administrative workspaces.
func canReviewEducationDirectory(u middleware.AuthUser) bool {
	if u.Role == models.RoleAdmin || u.Role == models.RoleModerator {
		return u.EntityType == models.EntityOrganization
	}
	return u.EntityType == models.EntityEduInst
}
func canAccessPartner(u middleware.AuthUser, id string) bool {
	if u.EntityType == models.EntityEduInst {
		return u.PartnerID != nil && *u.PartnerID == id && id != ""
	}
	return isStaff(u)
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
	if u.EntityType == models.EntityEduInst || (u.EntityType == "" && u.ITCompanyID == nil && (u.Role == models.RoleAdmin || u.Role == models.RoleModerator)) {
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
	if isStaff(u) {
		return requested
	}
	return "unassigned"
}
