package handlers

import (
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
	"cybercalc/internal/money"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPartnerAccess(t *testing.T) {
	p := "partner-a"
	own := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst, PartnerID: &p}
	educationAdmin := middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityEduInst, PartnerID: &p}
	tests := []struct {
		u    middleware.AuthUser
		p    string
		want bool
	}{
		{middleware.AuthUser{Role: models.RoleAdmin}, "partner-b", true},
		{middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityOrganization}, "partner-b", true},
		{middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, "partner-b", false},
		{middleware.AuthUser{Role: models.RoleCurator, EntityType: models.EntityOrganization, PartnerID: &p}, p, true},
		{middleware.AuthUser{Role: models.RoleCurator, EntityType: models.EntityOrganization, PartnerID: &p}, "partner-b", false},
		{own, p, true}, {own, "partner-b", false}, {middleware.AuthUser{Role: models.RoleUser}, p, false},
		{educationAdmin, p, true}, {educationAdmin, "partner-b", false},
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
	if partnerScope(educationAdmin, "partner-b") != p {
		t.Fatal("education admin query must remain scoped to the assigned institution")
	}
	organizationCurator := middleware.AuthUser{Role: models.RoleCurator, EntityType: models.EntityOrganization, PartnerID: &p}
	if partnerScope(organizationCurator, "partner-b") != p {
		t.Fatal("organization curator query must remain scoped to the assigned institution")
	}
}

func TestOrderBasedReportRoles(t *testing.T) {
	partner := "partner-a"
	education := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst, PartnerID: &partner}
	educationAdmin := middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityEduInst, PartnerID: &partner}
	educationModerator := middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityEduInst, PartnerID: &partner}
	organization := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}
	admin := middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityOrganization}

	if !canPrepareReports(organization) || !canPrepareReports(admin) {
		t.Fatal("the IT organization and operators must be able to prepare reports")
	}
	for _, user := range []middleware.AuthUser{education, educationAdmin, educationModerator} {
		if canPrepareReports(user) {
			t.Fatalf("an educational profile must review, not author, reports: %+v", user)
		}
		if !canReviewReport(user, "fact") || canReviewReport(user, "plan") {
			t.Fatalf("an educational profile reviews only a submitted fact report: %+v", user)
		}
		if canApproveReports(user) {
			t.Fatalf("an educational profile must not perform the IT organization's final approval: %+v", user)
		}
	}
	if canReviewReport(organization, "fact") {
		t.Fatal("a regular IT-organization user must not confirm its own counterparty review")
	}
	if !canReviewReport(admin, "fact") {
		t.Fatal("an operator must be able to record deemed counterparty approval")
	}
	unassigned := middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityEduInst}
	if isEducationRepresentative(unassigned) || canReviewReport(unassigned, "fact") {
		t.Fatal("an unassigned educational profile must not review a report")
	}
}

func TestExtendedRBACFieldBoundaries(t *testing.T) {
	organization := func(role models.Role) middleware.AuthUser {
		return middleware.AuthUser{Role: role, EntityType: models.EntityOrganization}
	}
	if !canEditEntryCategory(organization(models.RoleHRSpecialist), "internship") ||
		!canEditEntryCategory(organization(models.RoleHRSpecialist), "employment_practice") ||
		canEditEntryCategory(organization(models.RoleHRSpecialist), "teachers") {
		t.Fatal("HR specialist category boundary is incorrect")
	}
	if !canEditEntryCategory(organization(models.RoleFinancialSpecialist), "teachers") ||
		canEditEntryCategory(organization(models.RoleFinancialSpecialist), "top_it") {
		t.Fatal("financial specialist category boundary is incorrect")
	}
	if canCreateEntryCategory(organization(models.RoleFinancialSpecialist), "teachers") ||
		!canCreateEntryCategory(organization(models.RoleHRSpecialist), "internship") {
		t.Fatal("specialist create boundary is incorrect")
	}
	for _, role := range []models.Role{models.RoleLegalSpecialist, models.RoleAuditorViewer} {
		user := organization(role)
		if canEditAnyEntry(user) || canPrepareReports(user) || isStaff(user) {
			t.Fatalf("%s received a general write permission", role)
		}
		if !canReadTenantData(user) {
			t.Fatalf("%s lost tenant read access", role)
		}
	}
	if canUploadAnyDocument(organization(models.RoleAuditorViewer)) {
		t.Fatal("auditor must not upload documents")
	}
	educationCurator := middleware.AuthUser{Role: models.RoleCurator, EntityType: models.EntityEduInst}
	if !canUploadDocument(educationCurator, "internship", "outgoing_certificate") ||
		canUploadDocument(educationCurator, "internship", "labor_contract") {
		t.Fatal("education curator document boundary is incorrect")
	}
}

func TestFinancialUpdateAllowsOnlyCompensationFields(t *testing.T) {
	oldPayload := []byte(`{"org_name":"partner","course_name":"Курс","academic_hours":2,"payment_status":"planned"}`)
	req := updateEntryRequest{Payload: map[string]interface{}{
		"org_name": "partner", "course_name": "Курс", "academic_hours": float64(2),
		"payment_status": "paid", "payment_date": "2026-09-22", "payment_order_reference": "ПП-1",
	}}
	if !financialUpdateAllowed(oldPayload, req, "vuz", "agreement", "average", nil) {
		t.Fatal("valid compensation confirmation was rejected")
	}
	req.Payload["academic_hours"] = float64(3)
	if financialUpdateAllowed(oldPayload, req, "vuz", "agreement", "average", nil) {
		t.Fatal("financial role changed a calculation field")
	}
	req.Payload["academic_hours"] = float64(2)
	delete(req.Payload, "payment_order_reference")
	if financialUpdateAllowed(oldPayload, req, "vuz", "agreement", "average", nil) {
		t.Fatal("paid compensation without payment document was accepted")
	}
	actual := money.Amount(10000)
	req.ActualAmountRub = &actual
	if financialUpdateAllowed(oldPayload, req, "vuz", "agreement", "average", nil) {
		t.Fatal("financial role changed the report amount")
	}
}

func TestEducationRepresentativeCannotWriteReportData(t *testing.T) {
	partner := "partner-a"
	for _, role := range []models.Role{models.RoleOrgAdmin, models.RoleSuperAdmin} {
		u := middleware.AuthUser{Role: role, EntityType: models.EntityEduInst, PartnerID: &partner}
		entryHandlers := &EntryHandlers{}
		attachmentHandlers := &AttachmentHandlers{}
		tests := []struct {
			name   string
			handle func(http.ResponseWriter, *http.Request)
		}{
			{"create entry", func(w http.ResponseWriter, r *http.Request) { entryHandlers.Create(w, r, u) }},
			{"update entry", func(w http.ResponseWriter, r *http.Request) { entryHandlers.Update(w, r, u, "entry") }},
			{"import entries", func(w http.ResponseWriter, r *http.Request) { entryHandlers.Import(w, r, u) }},
			{"upload attachment", func(w http.ResponseWriter, r *http.Request) { attachmentHandlers.Upload(w, r, u, "entry") }},
		}
		for _, test := range tests {
			t.Run(string(role)+"/"+test.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				test.handle(w, httptest.NewRequest(http.MethodPost, "/", nil))
				if w.Code != http.StatusForbidden {
					t.Fatalf("status=%d, want 403", w.Code)
				}
			})
		}
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
		{"IT organization", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, true},
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

func TestEducationDirectoryProposalPermissions(t *testing.T) {
	tests := []struct {
		name    string
		user    middleware.AuthUser
		propose bool
		approve bool
	}{
		{"IT organization admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityOrganization}, false, true},
		{"IT organization moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityOrganization}, true, false},
		{"IT organization user", middleware.AuthUser{Role: models.RoleUser, EntityType: models.EntityOrganization}, false, false},
		{"education admin", middleware.AuthUser{Role: models.RoleAdmin, EntityType: models.EntityEduInst}, false, false},
		{"education moderator", middleware.AuthUser{Role: models.RoleModerator, EntityType: models.EntityEduInst}, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canProposeEducationDirectory(test.user); got != test.propose {
				t.Fatalf("canProposeEducationDirectory=%v, want %v", got, test.propose)
			}
			if got := canApproveEducationDirectory(test.user); got != test.approve {
				t.Fatalf("canApproveEducationDirectory=%v, want %v", got, test.approve)
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
		{"super administrator", middleware.AuthUser{Role: models.RoleSuperAdmin}, true},
		{"holding administrator", middleware.AuthUser{Role: models.RoleHoldingAdmin, EntityType: models.EntityOrganization}, true},
		{"organization administrator", middleware.AuthUser{Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization}, false},
		{"curator", middleware.AuthUser{Role: models.RoleCurator, EntityType: models.EntityOrganization}, false},
		{"auditor", middleware.AuthUser{Role: models.RoleAuditorViewer, EntityType: models.EntityOrganization}, false},
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
	for _, role := range []models.Role{models.RoleSuperAdmin, models.RoleHoldingAdmin, models.RoleOrgAdmin, models.RoleCurator, models.RoleHRSpecialist, models.RoleFinancialSpecialist, models.RoleLegalSpecialist, models.RoleAuditorViewer} {
		for _, entity := range []models.EntityType{models.EntityOrganization, models.EntityEduInst, ""} {
			u := middleware.AuthUser{Role: role, EntityType: entity}
			if got := canViewITCompanies(u); !got {
				t.Fatalf("%s / %s: valid RBAC role must have read access", role, entity)
			}
		}
	}
	if canViewITCompanies(middleware.AuthUser{Role: "unknown"}) {
		t.Fatal("unknown role must not have read access")
	}
}

func TestITCompanyHandlersEnforceProfileAccess(t *testing.T) {
	h := ITCompanyHandlers{}
	invalid := middleware.AuthUser{Role: "unknown", EntityType: models.EntityOrganization}
	w := httptest.NewRecorder()
	h.List(w, httptest.NewRequest("GET", "/it-companies", nil), invalid)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unknown role: list status=%d, want 403", w.Code)
	}

	u := middleware.AuthUser{Role: models.RoleOrgAdmin, EntityType: models.EntityOrganization}
	for _, handler := range []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request, middleware.AuthUser)
	}{{"create", h.Create}, {"import", h.Import}, {"template", h.Template}} {
		t.Run(handler.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.handle(w, httptest.NewRequest("GET", "/it-companies", nil), u)
			if w.Code != http.StatusForbidden {
				t.Fatalf("organization administrator: status=%d, want 403", w.Code)
			}
		})
	}
	reader := middleware.AuthUser{Role: models.RoleAuditorViewer, EntityType: models.EntityOrganization}
	for _, handler := range []func(http.ResponseWriter, *http.Request, middleware.AuthUser){h.Create, h.Import, h.Template} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("POST", "/it-companies", nil), reader)
		if w.Code != http.StatusForbidden {
			t.Fatalf("education reader: write status=%d, want 403", w.Code)
		}
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
