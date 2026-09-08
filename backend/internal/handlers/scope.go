package handlers

import (
	"strconv"

	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

// appendEntryScope restricts business data to the account that created it.
// A partner account is additionally restricted to its assigned partner.
// Administrators retain the cross-organization view required by the admin area.
func appendEntryScope(conditions []string, args []interface{}, u middleware.AuthUser, alias string) ([]string, []interface{}) {
	if u.Role == models.RoleAdmin {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	args = append(args, u.ID)
	conditions = append(conditions, column("created_by")+" = $"+strconv.Itoa(len(args)))
	if u.PartnerID != nil {
		args = append(args, *u.PartnerID)
		conditions = append(conditions, column("partner_id")+" = $"+strconv.Itoa(len(args)))
	}
	return conditions, args
}
