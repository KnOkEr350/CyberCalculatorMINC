package handlers

import (
	"cybercalc/internal/models"
	"testing"
)

func TestValidateAndNormalizeNewUser(t *testing.T) {
	req := createUserRequest{
		Email:    "  USER@Example.COM ",
		Password: "StrongPass1!",
		FullName: "  Иван   Иванов  ",
		Role:     string(models.RoleSuperAdmin),
	}
	if err := validateAndNormalizeNewUser(&req); err != nil {
		t.Fatalf("valid user rejected: %v", err)
	}
	if req.Email != "user@example.com" {
		t.Fatalf("email not normalized: %q", req.Email)
	}
	if req.FullName != "Иван Иванов" {
		t.Fatalf("full name not normalized: %q", req.FullName)
	}
}

func TestValidateAndNormalizeEveryRBACRole(t *testing.T) {
	itCompanyID := "11111111-1111-1111-1111-111111111111"
	roles := []models.Role{models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator, models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer}
	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			req := createUserRequest{Email: string(role) + "@example.com", Password: "StrongPass1!", FullName: "Иван Иванов", Role: string(role), EntityType: "organization"}
			if role != models.RoleSuperAdmin {
				req.ITCompanyID = &itCompanyID
			}
			if err := validateAndNormalizeNewUser(&req); err != nil {
				t.Fatalf("valid role rejected: %v", err)
			}
		})
	}
}

func TestValidateAndNormalizeOrganizationUserWithITCompany(t *testing.T) {
	itCompanyID := "11111111-1111-1111-1111-111111111111"
	req := createUserRequest{
		Email:       "company-user@example.com",
		Password:    "StrongPass1!",
		FullName:    "Иван Иванов",
		Role:        string(models.RoleCurator),
		EntityType:  "organization",
		ITCompanyID: &itCompanyID,
	}
	if err := validateAndNormalizeNewUser(&req); err != nil {
		t.Fatalf("valid IT company user rejected: %v", err)
	}
}

func TestValidateAndNormalizeNewUserRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		req  createUserRequest
	}{
		{"bad email", createUserRequest{Email: "wrong", Password: "StrongPass1!", FullName: "Иван", Role: string(models.RoleSuperAdmin)}},
		{"weak password", createUserRequest{Email: "user@example.com", Password: "password", FullName: "Иван", Role: string(models.RoleSuperAdmin)}},
		{"blank name", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: " ", Role: string(models.RoleSuperAdmin)}},
		{"bad role", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: "Иван", Role: "owner"}},
		{"education user without partner", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: "Иван", Role: string(models.RoleCurator), EntityType: "edu_institution"}},
		{"IT company user without company", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: "Иван", Role: string(models.RoleCurator), EntityType: "organization"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAndNormalizeNewUser(&tt.req); err == nil {
				t.Fatal("invalid user must be rejected")
			}
		})
	}
}
