package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"net/http"
	"net/http/httptest"
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
		{"IT organization admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityOrganization}, true},
		{"education admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityEduInst}, false},
		{"unassigned admin", middleware.AuthUser{Role: models.RoleAdmin}, false},
		{"IT organization moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityOrganization}, true},
		{"education moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityEduInst}, false},
		{"unassigned moderator", middleware.AuthUser{Role: models.RoleModerator}, false},
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

func TestEducationDirectoryHandlersRejectEducationManagers(t *testing.T) {
	h := PartnerHandlers{}
	for _, role := range []models.Role{models.RoleAdmin, models.RoleModerator} {
		u := middleware.AuthUser{Role: role, EntityType: models.EntityEduInst}
		for _, handler := range []struct {
			name   string
			handle func(http.ResponseWriter, *http.Request, middleware.AuthUser)
		}{
			{"list", h.Directory}, {"stats", h.DirectoryStats},
			{"template", h.DirectoryTemplate}, {"import", h.ImportDirectory},
			{"update", func(w http.ResponseWriter, r *http.Request, u middleware.AuthUser) {
				h.UpdateDirectory(w, r, u, "test-directory")
			}},
		} {
			t.Run(string(role)+"/"+handler.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				handler.handle(w, httptest.NewRequest("GET", "/directory", nil), u)
				if w.Code != http.StatusForbidden {
					t.Fatalf("status=%d, want 403", w.Code)
				}
			})
		}
	}
}

func TestITCompanyDirectoryAccess(t *testing.T) {
	tests := []struct {
		name string
		user middleware.AuthUser
		want bool
	}{
		{"education admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityEduInst}, true},
		{"IT organization admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityOrganization}, false},
		{"unassigned admin", middleware.AuthUser{Role: models.RoleAdmin}, false},
		{"IT organization moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityOrganization}, false},
		{"education moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityEduInst}, true},
		{"unassigned moderator", middleware.AuthUser{Role: models.RoleModerator}, false},
		{"IT organization user", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, true},
		{"education user", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canManageITCompanies(test.user); got != test.want {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}

func TestITCompanyReadAccess(t *testing.T) {
	for _, role := range []models.Role{models.RoleAdmin, models.RoleModerator, models.RoleUser} {
		for _, entity := range []models.EntityType{models.EntityOrganization, models.EntityEduInst, ""} {
			u := middleware.AuthUser{Role: role, EntityType: entity}
			want := entity == models.EntityEduInst || (role == models.RoleUser && entity == models.EntityOrganization)
			if got := canViewITCompanies(u); got != want {
				t.Fatalf("%s / %s: read access=%v, want %v", role, entity, got, want)
			}
		}
	}
}

func TestITCompanyHandlersEnforceProfileAccess(t *testing.T) {
	h := ITCompanyHandlers{}
	u := middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityOrganization}
	for _, handler := range []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request, middleware.AuthUser)
	}{{"list", h.List}, {"search", h.RegistrySearch}, {"create", h.Create}, {"import", h.Import}, {"template", h.Template}} {
		t.Run(handler.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.handle(w, httptest.NewRequest("GET", "/it-companies", nil), u)
			if w.Code != http.StatusForbidden {
				t.Fatalf("IT moderator: status=%d, want 403", w.Code)
			}
		})
	}
	reader := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst}
	for _, handler := range []func(http.ResponseWriter, *http.Request, middleware.AuthUser){h.Create, h.Import, h.Template} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/it-companies", nil), reader)
		if w.Code != http.StatusForbidden {
			t.Fatalf("education reader: write status=%d, want 403", w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.RegistrySearch(w, httptest.NewRequest("GET", "/it-companies/registry-search", nil), reader)
	if w.Code != http.StatusOK {
		t.Fatalf("education reader: search status=%d, want 200", w.Code)
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
