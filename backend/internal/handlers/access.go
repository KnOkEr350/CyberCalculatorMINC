package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"database/sql"
	"net/http"
)

// An organization profile represents the obligated IT organization and works
// across its educational partners. Admin and moderator are operator roles.
func isStaff(u middleware.AuthUser) bool {
	return u.Role == models.RoleAdmin || u.Role == models.RoleModerator || u.EntityType == models.EntityOrganization
}

// The Order assigns preparation of the plan and reports to the obligated IT
// organization. An educational organization is a counterparty: it may read its
// own materials and review a submitted fact report, but it must not author it.
func canPrepareReports(u middleware.AuthUser) bool {
	return isStaff(u)
}

func isEducationRepresentative(u middleware.AuthUser) bool {
	return u.Role == models.RoleUser && u.EntityType == models.EntityEduInst && u.PartnerID != nil && *u.PartnerID != ""
}

func canReviewReport(u middleware.AuthUser, period string) bool {
	if period != string(models.PeriodFact) {
		return isStaff(u)
	}
	// Administrators and moderators may record deemed approval after the
	// statutory response period. A regular IT-organization user cannot approve
	// the counterparty's own review.
	return isEducationRepresentative(u) || u.Role == models.RoleAdmin || u.Role == models.RoleModerator
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
	return isStaff(u) || (u.EntityType == models.EntityEduInst && u.PartnerID != nil && *u.PartnerID == id && id != "")
}
func requirePartner(w http.ResponseWriter, u middleware.AuthUser, id string) bool {
	if !canAccessPartner(u, id) {
		middleware.WriteError(w, 403, "нет доступа к выбранному учебному заведению")
		return false
	}
	return true
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
	return requirePartner(w, u, partner.String)
}
func partnerScope(u middleware.AuthUser, requested string) string {
	if isStaff(u) {
		return requested
	}
	if u.EntityType == models.EntityEduInst && u.PartnerID != nil {
		return *u.PartnerID
	}
	return "unassigned"
}
