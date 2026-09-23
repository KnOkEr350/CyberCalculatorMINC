package tests

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestComposeBoundary(t *testing.T) {
	s := newComposeSmoke(t)
	c := s.newClient(t)
	for _, path := range []string{"/", "/app.js", "/screens.js", "/style.css", "/api/live", "/api/ready", "/api/features", "/version.txt"} {
		t.Run(path, func(t *testing.T) {
			data := c.request(t, "GET", path, "", nil, http.StatusOK)
			switch path {
			case "/":
				if !bytes.Contains(data, []byte("Калькулятор затрат")) {
					t.Fatal("nginx did not serve the application frontend")
				}
			case "/api/live", "/api/ready":
				if decodeSmoke[struct{ Status string }](t, data).Status != "ok" {
					t.Fatalf("unhealthy response: %s", data)
				}
			case "/api/features":
				features := decodeSmoke[struct {
					Version int             `json:"version"`
					Flags   map[string]bool `json:"flags"`
				}](t, data)
				if features.Version != 1 || len(features.Flags) == 0 {
					t.Fatalf("invalid CI feature snapshot: %+v", features)
				}
				for name, enabled := range features.Flags {
					if !enabled {
						t.Fatalf("CI frontend feature %q is disabled; disposable smoke stack must enable all features", name)
					}
				}
			case "/version.txt":
				if got := strings.TrimSpace(string(data)); got != s.version {
					t.Fatalf("served revision %q, want %q", got, s.version)
				}
			}
		})
	}
	t.Run("worker", func(t *testing.T) {
		if got := strings.TrimSpace(s.compose(t, "", "ps", "--status", "running", "--services", "worker")); got != "worker" {
			t.Fatalf("worker is not running: %q", got)
		}
	})
	t.Run("authentication", func(t *testing.T) {
		// A registered protected route returns 401 before login. A 404 here
		// means the disposable stack forgot to enable its backend features.
		c.json(t, "GET", "/api/partners", nil, http.StatusUnauthorized)
		c.json(t, "GET", "/api/auth/me", nil, http.StatusUnauthorized)
		s.login(t, smokeAdminEmail, smokeAdminPassword)
	})
}

func TestComposeWorkspace(t *testing.T) {
	s := newComposeSmoke(t)
	admin := s.login(t, smokeAdminEmail, smokeAdminPassword)
	admin.json(t, "POST", "/api/auth/entity-type", map[string]string{"entity_type": "organization"}, http.StatusOK)
	tenantID := s.seedTenant(t)
	s.seedVerifiedTariff(t)
	partner := s.createPartner(t, admin)

	t.Run("create_edit_and_attachments", func(t *testing.T) {
		testComposeAttachments(t, admin, partner)
	})
	t.Run("moderator_boundaries_and_registry", func(t *testing.T) {
		testComposeOrgAdmins(t, s, admin, partner, tenantID)
	})
}

func testComposeAttachments(t *testing.T, admin *smokeClient, partner smokePartner) {
	payload := map[string]any{
		"org_name": partner.ID, "course_name": "CI course", "teacher_full_name": "CI teacher",
		"education_level": "bachelor", "semester": 1,
		"employment_form": "ГПХ", "academic_hours": 2,
	}
	created := decodeSmoke[struct{ ID string }](t, admin.json(t, "POST", "/api/entries", map[string]any{
		"category_code": "teachers", "partner_id": partner.ID, "agreement_id": partner.AgreementID,
		"period_type": "fact", "report_year": 2026, "audience": "vuz", "payload": payload,
	}, http.StatusCreated))
	entryID := requireSmokeID(t, "entry", created.ID)
	const first = "created with entry\n"
	const second = "attached after edit\n"
	admin.upload(t, entryID, "files", "first.txt", first)
	payload["course_name"] = "Updated CI course"
	payload["academic_hours"] = 3
	admin.json(t, "PUT", "/api/entries/"+entryID, map[string]any{
		"audience": "vuz", "agreement_id": partner.AgreementID,
		"comment": "CI attachment edit check", "payload": payload,
	}, http.StatusOK)
	admin.upload(t, entryID, "file", "second.txt", second)
	attachments := decodeSmoke[[]struct {
		ID       string `json:"id"`
		FileName string `json:"file_name"`
	}](t, admin.json(t, "GET", "/api/entries/"+entryID+"/attachments", nil, http.StatusOK))
	if len(attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(attachments))
	}
	expected := map[string]string{"first.txt": first, "second.txt": second}
	for _, attachment := range attachments {
		want, ok := expected[attachment.FileName]
		if !ok {
			t.Fatalf("unexpected or duplicate attachment: %q", attachment.FileName)
		}
		requireSmokeID(t, "attachment", attachment.ID)
		got := admin.request(t, "GET", "/api/attachments/"+attachment.ID+"/download", "", nil, http.StatusOK)
		if string(got) != want {
			t.Fatalf("downloaded %s differs from uploaded content", attachment.FileName)
		}
		delete(expected, attachment.FileName)
	}
}

func testComposeOrgAdmins(t *testing.T, s *composeSmoke, admin *smokeClient, partner smokePartner, tenantID string) {
	orgAdmin := s.createOrgAdmin(t, admin, "ci-org-admin@example.invalid", "organization", "", tenantID)
	for _, path := range []string{"it-companies", "it-companies/registry-search"} {
		orgAdmin.json(t, "GET", "/api/"+path, nil, http.StatusOK)
	}
	orgAdmin.json(t, "GET", "/api/it-companies/template", nil, http.StatusForbidden)
	for _, path := range []string{"it-companies", "it-companies/import"} {
		orgAdmin.json(t, "POST", "/api/"+path, nil, http.StatusForbidden)
	}
	orgAdmin.request(t, "GET", "/api/admin/directory-template", "", nil, http.StatusOK)
	// Resolve the partner through nginx as well as checking the creation response.
	partners := decodeSmoke[[]struct{ ID, Name string }](t, admin.json(t, "GET", "/api/partners", nil, http.StatusOK))
	found := false
	for _, item := range partners {
		if item.ID == partner.ID && item.Name == "CI test university" {
			found = true
		}
	}
	if !found {
		t.Fatal("created university missing from the administrator's workspace")
	}
	education := s.createOrgAdmin(t, admin, "ci-education-org-admin@example.invalid", "edu_institution", partner.ID, "")
	for _, client := range []*smokeClient{orgAdmin, education} {
		for _, path := range []string{"admin/users", "admin/settings", "admin/logs"} {
			client.json(t, "GET", "/api/"+path, nil, http.StatusForbidden)
		}
	}
	admin.json(t, "POST", "/api/it-companies", map[string]string{
		"name": "CI accredited IT company", "inn": "7707083893", "ogrn": "1027700132195",
		"accreditation_number": "CI-ACCREDITATION-1", "registry_record_id": "ci-it-company-1",
		"registry_updated_at": time.Now().UTC().Format(time.DateOnly),
		"source_url":          "https://digital.gov.ru/ru/activity/govservices/1/",
	}, http.StatusCreated)
	companies := decodeSmoke[[]struct {
		AccreditationStatus string `json:"accreditation_status"`
	}](t, education.json(t, "GET", "/api/it-companies?q=CI%20accredited", nil, http.StatusOK))
	if len(companies) != 1 || companies[0].AccreditationStatus != "active" {
		t.Fatalf("expected one active accredited company, got %+v", companies)
	}
}
