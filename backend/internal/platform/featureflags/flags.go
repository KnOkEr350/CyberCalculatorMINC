// Package featureflags provides an immutable, validated feature flag set.
package featureflags

import (
	"fmt"
	"sort"
	"strings"
)

type Name string

const (
	DashboardV44     Name = "dashboard_v44"
	PartnersV44      Name = "partners_v44"
	Teachers         Name = "teachers"
	OOPRPD           Name = "oop_rpd"
	Internships      Name = "internships"
	Practice         Name = "practice"
	TopITAI          Name = "top_it_ai"
	Schools          Name = "schools"
	MinistryDecision Name = "ministry_decision"
	ReportingV44     Name = "reporting_v44"
	SettingsV44      Name = "settings_v44"
)

var knownNames = []Name{
	DashboardV44,
	PartnersV44,
	Teachers,
	OOPRPD,
	Internships,
	Practice,
	TopITAI,
	Schools,
	MinistryDecision,
	ReportingV44,
	SettingsV44,
}

type Set struct {
	enabled map[Name]struct{}
	usedAll bool
}

// Parse accepts a comma-separated list. Empty input disables every flag;
// "all" explicitly enables the complete known registry.
func Parse(raw string) (Set, error) {
	set := Set{enabled: make(map[Name]struct{})}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return set, nil
	}

	known := make(map[Name]struct{}, len(knownNames))
	for _, name := range knownNames {
		known[name] = struct{}{}
	}

	var unknown []string
	for _, token := range strings.Split(raw, ",") {
		value := strings.TrimSpace(token)
		if value == "" {
			continue
		}
		if value == "all" {
			set.usedAll = true
			for name := range known {
				set.enabled[name] = struct{}{}
			}
			continue
		}
		name := Name(value)
		if _, ok := known[name]; !ok {
			unknown = append(unknown, value)
			continue
		}
		set.enabled[name] = struct{}{}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return Set{}, fmt.Errorf("неизвестные feature flags: %s", strings.Join(unknown, ", "))
	}
	return set, nil
}

func (s Set) UsedAllShortcut() bool {
	return s.usedAll
}

func (s Set) Enabled(name Name) bool {
	_, ok := s.enabled[name]
	return ok
}

// Snapshot returns all known flags, including disabled ones, and never exposes
// the internal map to callers.
func (s Set) Snapshot() map[string]bool {
	result := make(map[string]bool, len(knownNames))
	for _, name := range knownNames {
		result[string(name)] = s.Enabled(name)
	}
	return result
}

func Names() []Name {
	return append([]Name(nil), knownNames...)
}
