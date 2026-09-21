package config

import (
	"strings"
	"testing"

	"cybercalc/internal/platform/featureflags"
)

func TestLoadKeepsBackendAndFrontendFlagsIndependent(t *testing.T) {
	t.Setenv("BACKEND_FEATURE_FLAGS", "teachers")
	t.Setenv("FRONTEND_FEATURE_FLAGS", "schools")
	config := Load()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if !config.BackendFeatureFlags.Enabled(featureflags.Teachers) || config.BackendFeatureFlags.Enabled(featureflags.Schools) {
		t.Fatal("backend feature flags are not isolated")
	}
	if !config.FrontendFeatureFlags.Enabled(featureflags.Schools) || config.FrontendFeatureFlags.Enabled(featureflags.Teachers) {
		t.Fatal("frontend feature flags are not isolated")
	}
}

func TestValidateRejectsUnknownFeatureFlag(t *testing.T) {
	t.Setenv("BACKEND_FEATURE_FLAGS", "teachers_typo")
	config := Load()
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "BACKEND_FEATURE_FLAGS") || !strings.Contains(err.Error(), "teachers_typo") {
		t.Fatalf("got %v, want unknown flag validation error", err)
	}
}

func TestProductionRejectsAllFeatureShortcut(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("BACKEND_FEATURE_FLAGS", "all")
	config := Load()
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "shortcut all") {
		t.Fatalf("got %v, want production all validation error", err)
	}
}
