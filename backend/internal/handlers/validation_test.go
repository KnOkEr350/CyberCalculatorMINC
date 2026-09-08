package handlers

import "testing"

func TestValidateAndNormalizeNewUser(t *testing.T) {
	req := createUserRequest{
		Email:    "  USER@Example.COM ",
		Password: "StrongPass1!",
		FullName: "  Иван   Иванов  ",
		Role:     "user",
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

func TestValidateAndNormalizeNewUserRejectsInvalidData(t *testing.T) {
	tests := []struct {
		name string
		req  createUserRequest
	}{
		{"bad email", createUserRequest{Email: "wrong", Password: "StrongPass1!", FullName: "Иван", Role: "user"}},
		{"weak password", createUserRequest{Email: "user@example.com", Password: "password", FullName: "Иван", Role: "user"}},
		{"blank name", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: " ", Role: "user"}},
		{"bad role", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: "Иван", Role: "owner"}},
		{"education user without partner", createUserRequest{Email: "user@example.com", Password: "StrongPass1!", FullName: "Иван", Role: "user", EntityType: "edu_institution"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAndNormalizeNewUser(&tt.req); err == nil {
				t.Fatal("invalid user must be rejected")
			}
		})
	}
}
