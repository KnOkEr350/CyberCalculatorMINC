package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"testing"
)

func TestPartnerAccess(t *testing.T) {
	p := "partner-a"
	own := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst, PartnerID: &p}
	tests := []struct {
		u    middleware.AuthUser
		p    string
		want bool
	}{
		{middleware.AuthUser{Role: models.RoleAdmin}, "partner-b", true},
		{middleware.AuthUser{Role: models.RoleModerator}, "partner-b", true},
		{middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, "partner-b", true},
		{own, p, true}, {own, "partner-b", false}, {middleware.AuthUser{Role: models.RoleUser}, p, false},
		{middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst}, p, false},
	}
	for _, tt := range tests {
		if got := canAccessPartner(tt.u, tt.p); got != tt.want {
			t.Fatalf("%+v / %s: got %v", tt.u, tt.p, got)
		}
	}
	if partnerScope(own, "partner-b") != p {
		t.Fatal("query must not expand partner scope")
	}
}

func TestEducationDirectoryReviewAccess(t *testing.T) {
	tests := []struct {
		name string
		user middleware.AuthUser
		want bool
	}{
		{"admin", middleware.AuthUser{Role: models.RoleAdmin}, true},
		{"moderator", middleware.AuthUser{Role: models.RoleModerator}, true},
		{"education user", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst}, true},
		{"IT organization", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, false},
		{"unassigned", middleware.AuthUser{Role: models.RoleUser}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canReviewEducationDirectory(test.user); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
func TestObligations(t *testing.T) {
	for _, kind := range []string{"vuz", "kolledj", "school"} {
		for _, top := range []bool{false, true} {
			for _, teachers := range []bool{false, true} {
				for _, programs := range []bool{false, true} {
					s := obligations(kind, teachers, programs, top)
					want := 0
					if kind == "vuz" {
						if !teachers {
							want++
						}
						if !programs {
							want++
						}
					}
					if len(s.Missing) != want {
						t.Fatalf("wrong missing obligations %+v", s)
					}
				}
			}
		}
	}
}
func TestMentorNames(t *testing.T) {
	for _, n := range []string{"Иванов Иван Иванович", "Анна-Мария Петрова", "李 明"} {
		if !validMentorName(n) {
			t.Errorf("rejected %s", n)
		}
	}
	for _, n := range []string{"", "Иванов", "123 456", "Иванов\nИван", "<script> Иван"} {
		if validMentorName(n) {
			t.Errorf("accepted %s", n)
		}
	}
}
