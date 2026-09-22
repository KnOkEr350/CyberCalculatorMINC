package tests

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var smokeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func requireSmokeID(t *testing.T, label, id string) string {
	t.Helper()
	if !smokeUUID.MatchString(id) {
		t.Fatalf("%s: expected UUID, got %q", label, id)
	}
	return id
}

// psql runs inside the isolated database container: no exposed DB port and no
// dependency on a host PostgreSQL client. Values use psql's quoted variables.
func (s *composeSmoke) seedID(t *testing.T, query string) string {
	t.Helper()
	id := strings.TrimSpace(s.compose(t, query, "exec", "-T", "db", "psql",
		"--username", "cybercalc_ci", "--dbname", "cybercalc_ci",
		"--quiet", "--tuples-only", "--no-align", "--set", "ON_ERROR_STOP=1",
		"--set", "admin_email="+smokeAdminEmail, "--set", "fixture_id="+s.project))
	return requireSmokeID(t, "fixture result", id)
}

func (s *composeSmoke) seedTenant(t *testing.T) string {
	t.Helper()
	return s.seedID(t, `WITH company AS (
		INSERT INTO accredited_it_companies(
			name,inn,ogrn,accreditation_status,registry_record_id,
			registry_updated_at,source_url,created_by
		) VALUES (
			'CI tenant company','7736050003','1027700070518','active',
			:'fixture_id',CURRENT_DATE,'https://www.gosuslugi.ru/itorgs',
			(SELECT id FROM users WHERE email=:'admin_email')
		) RETURNING id
	)
	UPDATE users SET it_company_id=company.id
	FROM company WHERE email=:'admin_email' RETURNING company.id;`)
}

func (s *composeSmoke) seedEducationDirectory(t *testing.T) string {
	t.Helper()
	return s.seedID(t, `INSERT INTO education_directory(
		name,partner_kind,region,source,inn,ogrn,license_number,
		license_status,institution_status,registry_record_id,source_url,
		registry_updated_at,verified_at,verification_status,
		program_codes,programs_source_url,programs_checked_at,listed_in_mincifry_order_27
	) VALUES (
		'CI test university','vuz','г. Москва','CI verified fixture',
		'7707083893','1027700132195','CI-LICENSE-1','active','active',
		:'fixture_id','https://islod.obrnadzor.gov.ru/rlic/details/ci-fixture',
		CURRENT_DATE,now(),'verified',ARRAY['09.03.01'],
		'https://ci.example.invalid/sveden/education/',now(),TRUE
	) RETURNING id;`)
}

type smokePartner struct {
	ID          string `json:"id"`
	AgreementID string `json:"agreement_id"`
}

func (s *composeSmoke) createPartner(t *testing.T, admin *smokeClient) smokePartner {
	t.Helper()
	data := admin.json(t, "POST", "/api/partners", map[string]any{
		"directory_id": s.seedEducationDirectory(t),
		"initial_agreement": map[string]any{
			"partner_ids": []string{}, "agreement_kind": "education_organization",
			"number": "CI-1", "status": "active", "signed_on": "2026-01-01",
			"valid_from": "2020-01-01", "valid_until": "2100-12-31",
			"signature_method": "qualified_electronic", "signed_by": "CI Signer",
			"signature_date": "2026-01-01", "document_reference": "CI smoke test",
			"responsible_people": []map[string]string{
				{"party": "cyberprotect", "full_name": "CI Administrator"},
				{"party": "counterparty", "full_name": "CI Partner"},
			},
		},
	}, http.StatusCreated)
	partner := decodeSmoke[smokePartner](t, data)
	requireSmokeID(t, "partner", partner.ID)
	requireSmokeID(t, "agreement", partner.AgreementID)
	return partner
}

func (s *composeSmoke) createOrgAdmin(t *testing.T, admin *smokeClient, email, entityType, partnerID, companyID string) *smokeClient {
	t.Helper()
	const password = "ModeratorPass1!"
	payload := map[string]string{
		"email": email, "password": password, "full_name": "CI Organization Administrator",
		"role": "org_admin", "entity_type": entityType,
	}
	if partnerID != "" {
		payload["partner_id"] = partnerID
	}
	if companyID != "" {
		payload["it_company_id"] = companyID
	}
	admin.json(t, "POST", "/api/admin/users", payload, http.StatusCreated)
	return s.login(t, email, password)
}
