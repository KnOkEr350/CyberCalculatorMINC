package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"database/sql"
	"net/http"
)

// organization denotes Cyberprotect staff, never an external partner company.
func isStaff(u middleware.AuthUser) bool {
	return u.Role == models.RoleAdmin || u.EntityType == models.EntityOrganization
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
