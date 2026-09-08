package handlers

import (
	"strconv"

	"cybercalc/internal/middleware"
)

// appendEntryScope uses the partner-level access model, not record authorship.
// Coworkers assigned to one educational partner share that partner's records.
func appendEntryScope(conditions []string, args []interface{}, u middleware.AuthUser, alias string) ([]string, []interface{}) {
	scope := partnerScope(u, "")
	if scope == "" {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	args = append(args, scope)
	conditions = append(conditions, column("partner_id")+"::text = $"+strconv.Itoa(len(args)))
	return conditions, args
}
