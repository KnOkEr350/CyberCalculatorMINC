package tests

import (
	"bytes"
	"cybercalc/internal/auth"
	"cybercalc/internal/config"
	"cybercalc/internal/dbx"
	appserver "cybercalc/internal/server"
	"cybercalc/internal/xlsx"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Run only against the isolated local test database, after migrations.
func TestWorkspaceIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("use the isolated workspace_test database")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	dir := os.Getenv("TEST_MIGRATIONS_DIR")
	if dir == "" {
		t.Fatal("TEST_MIGRATIONS_DIR required")
	}
	if err := dbx.RunMigrations(db, dir); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{UploadDir: t.TempDir(), MFAKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", BackendFeatureFlags: allBackendFeatures(t)}
	server := httptest.NewServer(appserver.BuildRoutes(db, cfg))
	defer server.Close()
	stamp := fmt.Sprint(time.Now().UnixNano())
	password := "WorkspaceTest1!"
	hash, _ := auth.HashPassword(password)
	email := "test" + stamp + "@workspace.test"
	if _, e := db.Exec(`INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES($1,$2,'Тест Администратор','super_admin','organization')`, email, hash); e != nil {
		t.Fatal(e)
	}
	newClient := func() *http.Client { jar, _ := cookiejar.New(nil); return &http.Client{Jar: jar} }
	admin, partnerClient, companyClient := newClient(), newClient(), newClient()
	call := func(client *http.Client, method, path string, body interface{}, want int) []byte {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, _ := http.NewRequest(method, server.URL+"/api"+path, bytes.NewReader(data))
		req.Header.Set("X-Cybercalc-Request", "1")
		req.Header.Set("Content-Type", "application/json")
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, want, b)
		}
		return b
	}
	upload := func(client *http.Client, path string, files map[string][]byte, want int) []byte {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for name, b := range files {
			f, _ := mw.CreateFormFile("file", name)
			f.Write(b)
		}
		mw.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api"+path, &body)
		req.Header.Set("X-Cybercalc-Request", "1")
		req.Header.Set("Content-Type", mw.FormDataContentType())
		res, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("upload %s: %d expected %d %s", path, res.StatusCode, want, b)
		}
		return b
	}
	object := func(b []byte) map[string]interface{} {
		t.Helper()
		var m map[string]interface{}
		if e := json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	money := func(value interface{}) string {
		t.Helper()
		amount, ok := value.(string)
		if !ok {
			t.Fatalf("money value must use the decimal string API contract, got %T (%v)", value, value)
		}
		return amount
	}
	call(admin, "POST", "/auth/login", map[string]string{"email": email, "password": password}, 200)
	var itCompanyID string
	if err := db.QueryRow(`INSERT INTO accredited_it_companies(name,inn,ogrn,accreditation_status,registry_updated_at,source_url,registry_record_id,created_by)
		VALUES($1,'7736050003','1027700070518','active',CURRENT_DATE,'https://www.gosuslugi.ru/itorgs',$2,(SELECT id FROM users WHERE email=$3))
		ON CONFLICT (inn) DO UPDATE SET name=EXCLUDED.name,accreditation_status='active' RETURNING id`, "А Тестовая ИТ-компания "+stamp, "test-"+stamp, email).Scan(&itCompanyID); err != nil {
		t.Fatal(err)
	}
	call(admin, "GET", "/it-companies", nil, 200)
	call(admin, "POST", "/it-companies", map[string]string{}, 400)
	if options := call(admin, "GET", "/admin/it-company-options", nil, 200); !bytes.Contains(options, []byte(itCompanyID)) || bytes.Contains(options, []byte("source_url")) {
		t.Fatal("assignment options must include the company without registry details")
	}
	companyEmail := "company" + stamp + "@workspace.test"
	companyUser := map[string]interface{}{
		"email": companyEmail, "password": password, "full_name": "Представитель ИТ-компании",
		"role": "curator", "entity_type": "organization",
	}
	call(admin, "POST", "/admin/users", companyUser, 400)
	companyUser["it_company_id"] = itCompanyID
	companyUserID := object(call(admin, "POST", "/admin/users", companyUser, 201))["id"].(string)
	call(companyClient, "POST", "/auth/login", map[string]string{"email": companyEmail, "password": password}, 200)
	call(companyClient, "GET", "/admin/it-company-options", nil, 403)
	companyProfile := object(call(companyClient, "GET", "/auth/me", nil, 200))
	if companyProfile["it_company_id"] != itCompanyID || companyProfile["partner_id"] != nil {
		t.Fatal("IT company assignment is missing or mixed with an educational institution")
	}
	if _, err := db.Exec(`UPDATE users SET it_company_id=$1 WHERE email=$2`, itCompanyID, email); err != nil {
		t.Fatal(err)
	}
	call(admin, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "savings_base_rub": 200000, "target_amount_rub": 5999, "source_reference": "Уведомление Минцифры", "notified_at": "2026-07-31"}, 400)
	call(admin, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "savings_base_rub": 166666.67, "target_amount_rub": 5000, "source_reference": "Уведомление Минцифры № 1", "notified_at": "2026-07-31"}, 200)
	call(admin, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "savings_base_rub": 200000, "target_amount_rub": 6000, "source_reference": "Уведомление Минцифры № 2", "notified_at": "2026-07-31"}, 200)
	adminDashboard := object(call(admin, "GET", "/dashboard?report_year=2026", nil, 200))
	if money(adminDashboard["target_amount_rub"]) != "6000.00" {
		t.Fatal("owner-scoped target upsert failed")
	}
	var directory1, directory2, schoolDirectory, staleDirectory string
	if e := db.QueryRow(`INSERT INTO education_directory(name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,registry_record_id,source_url,registry_updated_at,verified_at,verification_status)
		VALUES($1,'vuz','Республика Татарстан','https://islod.obrnadzor.gov.ru','1650084264','1021602020384','Л035-ТЕСТ-1','active','active',$2,'https://islod.obrnadzor.gov.ru/rlic/details/test-1',CURRENT_DATE,now(),'verified') RETURNING id`, "Тестовый вуз A "+stamp, "test-a-"+stamp).Scan(&directory1); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO education_directory(name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,registry_record_id,source_url,registry_updated_at,verified_at,verification_status)
		VALUES($1,'vuz','г.Москва','https://islod.obrnadzor.gov.ru','7707083893','1027700132195','Л035-ТЕСТ-2','active','active',$2,'https://islod.obrnadzor.gov.ru/rlic/details/test-2',CURRENT_DATE,now(),'verified') RETURNING id`, "Тестовый вуз B "+stamp, "test-b-"+stamp).Scan(&directory2); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO education_directory(name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,registry_record_id,source_url,registry_updated_at,verified_at,verification_status)
		VALUES($1,'school','Тестовый регион','https://islod.obrnadzor.gov.ru','7736050003','1027700070518','Л035-ШКОЛА','active','active',$2,'https://islod.obrnadzor.gov.ru/rlic/details/test-school',CURRENT_DATE,now(),'verified') RETURNING id`, "Тестовая школа "+stamp, "test-school-"+stamp).Scan(&schoolDirectory); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO education_directory(name,partner_kind,region,source,inn,ogrn,license_number,license_status,institution_status,registry_record_id,source_url,registry_updated_at,verified_at,verification_status)
		VALUES($1,'vuz','г.Москва','https://islod.obrnadzor.gov.ru','7707083893','1027700132195','Л035-УСТАРЕЛА','active','active',$2,'https://islod.obrnadzor.gov.ru/rlic/details/stale',CURRENT_DATE-36,now(),'verified') RETURNING id`, "Устаревшая запись "+stamp, "test-stale-"+stamp).Scan(&staleDirectory); e != nil {
		t.Fatal(e)
	}
	agreement := func(number string) map[string]interface{} {
		return map[string]interface{}{
			"agreement_kind": "education_organization", "number": number, "status": "active",
			"signed_on": "2026-01-01", "valid_from": "2026-01-01", "valid_until": "2026-12-31",
			"signature_method": "qualified_electronic", "signed_by": "Иванов Иван Иванович", "signature_date": "2026-01-01",
			"company_signer_authority": "Устав", "counterparty_signer_name": "Сидоров Сидор Сидорович",
			"counterparty_signer_position": "Ректор", "counterparty_signer_authority": "Устав",
			"responsible_people": []map[string]string{{"party": "cyberprotect", "full_name": "Петров Пётр Петрович"}, {"party": "counterparty", "full_name": "Сидоров Сидор Сидорович"}},
			"activity_codes":     []string{"teachers", "ood_rpd", "internship", "top_it"},
		}
	}
	// Verified licences alone are insufficient; an exact program is required.
	call(admin, "POST", "/partners", map[string]interface{}{"directory_id": directory1, "initial_agreement": agreement("NO-PROGRAM-" + stamp)}, 409)
	if _, e := db.Exec(`UPDATE education_directory SET program_codes=ARRAY['09.03.01'],programs_source_url='https://university.example/sveden/education/',listed_in_mincifry_order_27=TRUE WHERE id IN ($1,$2)`, directory1, directory2); e != nil {
		t.Fatal(e)
	}
	if _, e := db.Exec(`UPDATE education_directory SET program_codes=ARRAY['38.03.01'] WHERE id=$1`, staleDirectory); e != nil {
		t.Fatal(e)
	}
	programFiltered := call(admin, "GET", "/directory?partner_kind=vuz&q="+stamp, nil, 200)
	if bytes.Contains(programFiltered, []byte(staleDirectory)) || !bytes.Contains(programFiltered, []byte(directory1)) {
		t.Fatal("directory does not filter exact program codes")
	}
	if all := call(admin, "GET", "/directory?review_all=1&partner_kind=vuz&q="+stamp, nil, 200); !bytes.Contains(all, []byte(staleDirectory)) {
		t.Fatal("review queue lost excluded university")
	}
	created1 := object(call(admin, "POST", "/partners", map[string]interface{}{"directory_id": directory1, "initial_agreement": agreement("A-" + stamp)}, 201))
	created2 := object(call(admin, "POST", "/partners", map[string]interface{}{"directory_id": directory2, "initial_agreement": agreement("B-" + stamp)}, 201))
	call(admin, "POST", "/partners", map[string]interface{}{"directory_id": staleDirectory, "initial_agreement": agreement("STALE-" + stamp)}, 409)
	p1, agreement1 := created1["id"].(string), created1["agreement_id"].(string)
	p2, agreement2 := created2["id"].(string), created2["agreement_id"].(string)
	call(admin, "PATCH", "/admin/users/"+companyUserID, map[string]interface{}{"partner_id": p1}, 200)
	call(companyClient, "POST", "/auth/login", map[string]string{"email": companyEmail, "password": password}, 200)
	badSchoolAgreement := agreement("BAD-SCHOOL-" + stamp)
	call(admin, "POST", "/partners", map[string]interface{}{"directory_id": schoolDirectory, "initial_agreement": badSchoolAgreement}, 400)
	authorityBody := map[string]interface{}{
		"name": "Министерство образования тестового региона " + stamp, "region": "Тестовый регион",
		"inn": "7707083893", "ogrn": "1027700132195", "status": "active",
		"source_url": "https://education.test.gov.ru/regional-authority",
	}
	authorityID := object(call(admin, "POST", "/regional-authorities", authorityBody, 201))["id"].(string)
	schoolAgreement := agreement("SCHOOL-" + stamp)
	schoolAgreement["agreement_kind"] = "roiv"
	schoolAgreement["regional_authority_id"] = authorityID
	schoolAgreement["activity_codes"] = []string{"it_clubs"}
	createdSchool := object(call(admin, "POST", "/partners", map[string]interface{}{"directory_id": schoolDirectory, "initial_agreement": schoolAgreement}, 201))
	schoolPartner, schoolAgreementID := createdSchool["id"].(string), createdSchool["agreement_id"].(string)
	invalidROIVAgreement := agreement("BAD-ROIV-" + stamp)
	invalidROIVAgreement["agreement_kind"] = "roiv"
	invalidROIVAgreement["regional_authority_id"] = authorityID
	invalidROIVAgreement["partner_ids"] = []string{p1}
	call(admin, "POST", "/agreements", invalidROIVAgreement, 400)
	schoolEntry := map[string]interface{}{
		"partner_id": schoolPartner, "agreement_id": schoolAgreementID, "category_code": "it_clubs",
		"period_type": "plan", "report_year": 2026, "audience": "school",
		"payload": map[string]interface{}{
			"org_name": schoolPartner, "program_name": "Кружок ИТ", "academic_hours": 1, "developed_programs_count": 1, "students_count": 10,
			"funding_source": "100% средства ИТ-компании", "budget_funding": "absent", "citizen_funding": "absent",
		},
	}
	schoolEntry["payload"].(map[string]interface{})["budget_funding"] = "full_or_partial"
	call(admin, "POST", "/entries", schoolEntry, 400)
	schoolEntry["payload"].(map[string]interface{})["budget_funding"] = "absent"
	call(admin, "POST", "/entries", schoolEntry, 201)
	authorities := call(admin, "GET", "/regional-authorities", nil, 200)
	if !bytes.Contains(authorities, []byte(authorityID)) || !bytes.Contains(authorities, []byte(`"schools_count":1`)) || !bytes.Contains(authorities, []byte(`"activities_count":1`)) {
		t.Fatal("ROIV to school to activity relation is not visible")
	}
	authorityBody["status"] = "inactive"
	call(admin, "PUT", "/regional-authorities/"+authorityID, authorityBody, 200)
	call(admin, "POST", "/entries", schoolEntry, 400)
	authorityBody["status"] = "active"
	call(admin, "PUT", "/regional-authorities/"+authorityID, authorityBody, 200)
	groupAgreement := agreement("GROUP-" + stamp)
	groupAgreement["partner_ids"] = []string{p1, p2}
	groupAgreement["legal_entity_group"] = "Тестовая группа юридических лиц"
	groupID := object(call(admin, "POST", "/agreements", groupAgreement, 201))["id"].(string)
	listedAgreements := call(admin, "GET", "/agreements?partner_id="+p1, nil, 200)
	if !bytes.Contains(listedAgreements, []byte(agreement1)) || !bytes.Contains(listedAgreements, []byte(groupID)) || bytes.Contains(listedAgreements, []byte(agreement2)) {
		t.Fatal("agreement-to-partner scope is broken")
	}
	groupAgreement["status"] = "suspended"
	call(admin, "PUT", "/agreements/"+groupID, groupAgreement, 200)
	partnerEmail := "partner" + stamp + "@workspace.test"
	call(admin, "POST", "/admin/users", map[string]interface{}{"email": partnerEmail, "password": password, "full_name": "Представитель Вуза", "role": "curator", "entity_type": "edu_institution", "partner_id": p1}, 201)
	educationAdminEmail := "education-admin" + stamp + "@workspace.test"
	call(admin, "POST", "/admin/users", map[string]interface{}{"email": educationAdminEmail, "password": password, "full_name": "Администратор Вуза", "role": "org_admin", "entity_type": "edu_institution", "partner_id": p1}, 201)
	usersByName := call(admin, "GET", "/admin/users?q="+url.QueryEscape("Представитель Вуза"), nil, 200)
	if !bytes.Contains(usersByName, []byte(partnerEmail)) || bytes.Contains(usersByName, []byte(email)) {
		t.Fatal("admin user search by full name returned the wrong users")
	}
	usersByEmail := call(admin, "GET", "/admin/users?q="+url.QueryEscape(strings.ToUpper(partnerEmail)), nil, 200)
	if !bytes.Contains(usersByEmail, []byte(partnerEmail)) {
		t.Fatal("admin user search by email must be case-insensitive")
	}
	usersByWildcard := call(admin, "GET", "/admin/users?q="+url.QueryEscape("%_"), nil, 200)
	if bytes.Contains(usersByWildcard, []byte(partnerEmail)) || bytes.Contains(usersByWildcard, []byte(email)) {
		t.Fatal("admin user search must treat SQL wildcard characters literally")
	}
	call(admin, "GET", "/admin/users?q="+strings.Repeat("я", 201), nil, 400)
	call(partnerClient, "POST", "/auth/login", map[string]string{"email": partnerEmail, "password": password}, 200)
	educationAdminClient := newClient()
	call(educationAdminClient, "POST", "/auth/login", map[string]string{"email": educationAdminEmail, "password": password}, 200)
	educationAdminProfile := object(call(educationAdminClient, "GET", "/auth/me", nil, 200))
	if educationAdminProfile["role"] != "org_admin" || educationAdminProfile["entity_type"] != "edu_institution" || educationAdminProfile["partner_id"] != p1 {
		t.Fatal("education admin profile lost its assigned institution")
	}
	call(partnerClient, "POST", "/auth/entity-type", map[string]string{"entity_type": "organization"}, 403)
	call(partnerClient, "POST", "/partners", map[string]interface{}{"directory_id": directory2, "initial_agreement": agreement("foreign")}, 403)
	call(partnerClient, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "savings_base_rub": 33333.33, "target_amount_rub": 1000, "source_reference": "Уведомление", "notified_at": "2026-07-31"}, 403)
	reviewBody := map[string]interface{}{
		"name": "Тестовый вуз B " + stamp, "partner_kind": "vuz", "region": "г. Москва",
		"inn": "7707083893", "ogrn": "1027700132195", "license_number": "Л035-ТЕСТ-2",
		"license_status": "active", "institution_status": "active",
		"source_url":          "https://islod.obrnadzor.gov.ru/rlic/details/test-2",
		"registry_updated_at": time.Now().Format("2006-01-02"), "confirmation_comment": "Проверено представителем",
	}
	call(partnerClient, "PUT", "/directory/"+directory2, reviewBody, 403)
	call(admin, "PUT", "/directory/"+directory2, reviewBody, 200)
	var reviewStatus string
	if e := db.QueryRow(`SELECT verification_status FROM education_directory WHERE id=$1`, directory2).Scan(&reviewStatus); e != nil || reviewStatus != "pending" {
		t.Fatalf("directory edit must require confirmation: status=%s err=%v", reviewStatus, e)
	}
	reviewBody["confirm"] = true
	call(admin, "PUT", "/directory/"+directory2, reviewBody, 200)
	var confirmedBy string
	if e := db.QueryRow(`SELECT verification_status,verified_by::text FROM education_directory WHERE id=$1`, directory2).Scan(&reviewStatus, &confirmedBy); e != nil || reviewStatus != "verified" || confirmedBy == "" {
		t.Fatalf("directory confirmation was not attributed: status=%s by=%s err=%v", reviewStatus, confirmedBy, e)
	}
	audit := call(admin, "GET", "/admin/logs?entity_type=education_directory&limit=20", nil, 200)
	if !bytes.Contains(audit, []byte(`"action":"directory_confirm"`)) || !bytes.Contains(audit, []byte(email)) {
		t.Fatal("directory confirmation actor/details missing from audit")
	}
	partners := call(partnerClient, "GET", "/partners", nil, 200)
	if !bytes.Contains(partners, []byte(p1)) || bytes.Contains(partners, []byte(p2)) {
		t.Fatal("partner list leaks data")
	}
	educationAdminPartners := call(educationAdminClient, "GET", "/partners", nil, 200)
	if !bytes.Contains(educationAdminPartners, []byte(p1)) || bytes.Contains(educationAdminPartners, []byte(p2)) {
		t.Fatal("education admin can select an institution outside its assignment")
	}
	call(educationAdminClient, "POST", "/entries", map[string]interface{}{
		"partner_id": p1, "agreement_id": agreement1, "category_code": "teachers", "period_type": "plan", "report_year": 2026, "audience": "vuz",
		"payload": map[string]interface{}{"org_name": p1, "course_name": "Недопустимый план", "education_level": "bachelor", "semester": 1, "teacher_full_name": "Петров Пётр", "employment_form": "ГПХ", "academic_hours": 1},
	}, 403)
	educationAdminEntries := call(educationAdminClient, "GET", "/entries?report_year=2026", nil, 200)
	if bytes.Contains(educationAdminEntries, []byte(schoolPartner)) {
		t.Fatal("education admin sees another institution's entries")
	}
	educationAdminWorkflowPath := "/report-workflow?agreement_id=" + agreement1 + "&report_year=2026&period_type=plan"
	educationAdminWorkflow := object(call(educationAdminClient, "GET", educationAdminWorkflowPath, nil, 200))
	if educationAdminWorkflow["can_mark_ready"].(bool) {
		t.Fatal("education admin can prepare the plan")
	}
	call(educationAdminClient, "POST", "/report-workflow/transition?agreement_id="+agreement1+"&report_year=2026&period_type=plan", map[string]interface{}{
		"status": "ready", "scope_confirmed": true, "conditions_confirmed": true, "evidence_confirmed": true, "comment": "Недопустимая отправка плана",
	}, 409)
	mentor := object(call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p1, "full_name": "Иванов Иван Иванович"}, 201))["id"].(string)
	call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p2, "full_name": "Иванов Иван Иванович"}, 403)
	call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p1, "full_name": "123"}, 400)
	teacher := func(p string) map[string]interface{} {
		return map[string]interface{}{
			"org_name": p, "course_name": "ИТ", "education_level": "bachelor", "semester": 1,
			"teacher_full_name": "Петров Пётр", "employment_form": "ГПХ", "academic_hours": 2,
			"it_experience_days": 365, "okz_code": "2512", "employment_contract_reference": "ГПХ-1",
			"appointment_order_reference": "Приказ-1", "individual_plan_reference": "План-1", "class_schedule": "По расписанию",
		}
	}
	create := func(client *http.Client, p, agreementID, category, period string, payload map[string]interface{}, want int) []byte {
		return call(client, "POST", "/entries", map[string]interface{}{"partner_id": p, "agreement_id": agreementID, "category_code": category, "period_type": period, "report_year": 2026, "audience": "vuz", "payload": payload}, want)
	}
	create(partnerClient, p1, agreement1, "teachers", "plan", teacher(p1), 403)
	first := object(create(companyClient, p1, agreement1, "teachers", "plan", teacher(p1), 201))
	id := first["id"].(string)
	if money(first["amount_rub"]) != "8280.00" {
		t.Fatal("wrong teacher formula")
	}
	invalidSemester := teacher(p1)
	invalidSemester["semester"] = 9
	create(companyClient, p1, agreement1, "teachers", "plan", invalidSemester, 400)
	create(companyClient, p1, agreement2, "teachers", "plan", teacher(p1), 400)
	create(companyClient, p1, groupID, "teachers", "plan", teacher(p1), 400)
	foreign := object(create(admin, p2, agreement2, "teachers", "fact", teacher(p2), 201))["id"].(string)
	create(companyClient, p2, agreement2, "teachers", "plan", teacher(p2), 403)
	shared := object(create(admin, p1, agreement1, "teachers", "plan", teacher(p1), 201))["id"].(string)
	create(partnerClient, p2, agreement2, "teachers", "plan", teacher(p2), 403)
	call(partnerClient, "PUT", "/entries/"+foreign, map[string]interface{}{"payload": teacher(p2), "comment": "изменение"}, 403)
	call(partnerClient, "GET", "/entries/"+foreign+"/comments", nil, 403)
	call(partnerClient, "GET", "/entries/"+foreign+"/attachments", nil, 403)
	listed := call(partnerClient, "GET", "/entries?report_year=2026", nil, 200)
	if bytes.Contains(listed, []byte(foreign)) {
		t.Fatal("entry list leaks data")
	}
	if !bytes.Contains(listed, []byte(shared)) {
		t.Fatal("representatives must share their partner's entries, not only see their own")
	}
	companyListed := call(companyClient, "GET", "/entries?report_year=2026", nil, 200)
	if bytes.Contains(companyListed, []byte(foreign)) || !bytes.Contains(companyListed, []byte(shared)) {
		t.Fatal("organization curator scope is not limited to the assigned educational organization")
	}
	call(partnerClient, "GET", "/entries?report_year=oops", nil, 400)
	internship := map[string]interface{}{
		"org_name": p1, "mentor_id": mentor, "mentor_full_name": "Поддельное Имя", "student_full_name": "Сидоров Сидор",
		"duration_months": 2, "student_load_hours_per_month": 10, "mentor_load_hours_per_month": 3,
		"mentor_assignment_start": "2026-01-01", "mentor_assignment_end": "2026-12-31",
		"mentor_order_number": "Приказ-2", "mentor_order_date": "2025-12-30",
		"internship_agreement_reference": "Соглашение-1", "mentor_order_reference": "Приказ-2",
		"individual_program_reference": "Программа-1", "incoming_certificate_reference": "Справка-вход",
		"outgoing_certificate_reference": "Справка-итог",
	}
	trainee := object(create(companyClient, p1, agreement1, "internship", "fact", internship, 201))
	traineeID := trainee["id"].(string)
	if money(trainee["amount_rub"]) != "30340.00" {
		t.Fatal("wrong internship formula")
	}
	if _, err := db.Exec(`INSERT INTO agreement_activity_requirements(agreement_id,category_code) VALUES($1,'employment_practice')`, agreement1); err != nil {
		t.Fatalf("enable isolated employment practice fixture: %v", err)
	}
	practiceHeaders := []string{
		"mentor_full_name", "student_full_name", "course", "specialty_code", "period_start", "period_end",
		"duration_months", "student_load_hours_per_month", "mentor_load_hours_per_month",
		"labor_contract_type", "labor_contract_number", "labor_contract_date",
		"mentor_assignment_start", "mentor_assignment_end", "mentor_order_number", "mentor_order_date",
		"Номер договора о практической подготовке", "Дата договора о практической подготовке",
		// PRA-04: возраст и недельные часы обязательны — без них нечем
		// подтвердить нормы ТК РФ (ст. 63, 92), и практика к зачёту не
		// принимается.
		"student_age", "weekly_hours",
	}
	practiceWorkbook := func(contractType string) []byte {
		wb := xlsx.New()
		wb.AddSheet("Данные", practiceHeaders, [][]interface{}{{
			"Иванов Иван Иванович", "Практикантов Павел", "3", "09.03.01", "2026-09-01", "2026-09-30",
			1, 10, 3, contractType, "ТД-42", "2026-09-01",
			"2026-09-01", "2026-09-30", "12-ОК", "2026-08-30",
			"ПР-12", "2026-05-15", 19, 30,
		}})
		book, err := wb.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return book
	}
	practiceImportPath := "/entries/import?partner_id=" + p1 + "&agreement_id=" + agreement1 + "&category_code=employment_practice&period_type=fact&report_year=2026"
	invalidPracticeImport := object(upload(companyClient, practiceImportPath, map[string][]byte{"practice.xlsx": practiceWorkbook("Другой тип договора")}, 200))
	if len(invalidPracticeImport["errors"].([]interface{})) != 1 {
		t.Fatal("employment practice import accepted a non-fixed-term contract")
	}
	validPracticeImport := object(upload(companyClient, practiceImportPath, map[string][]byte{"practice.xlsx": practiceWorkbook("Срочный трудовой договор")}, 200))
	if len(validPracticeImport["errors"].([]interface{})) != 0 || money(validPracticeImport["total_rub"]) != "15170.00" {
		t.Fatal("employment practice import rejected a fixed-term contract")
	}
	practice := map[string]interface{}{
		"org_name": p1, "mentor_id": mentor, "student_full_name": "Практикантов Павел", "duration_months": 1,
		"course": "3", "specialty_code": "09.03.01", "period_start": "2026-09-01", "period_end": "2026-09-30",
		"student_load_hours_per_month": 10, "mentor_load_hours_per_month": 3,
		"mentor_assignment_start": "2026-09-01", "mentor_assignment_end": "2026-09-30",
		"mentor_order_number": "12-ОК", "mentor_order_date": "2026-08-30",
		// PRA-04: возраст и недельные часы обязательны для практики.
		"student_age": 19, "weekly_hours": 30,
	}
	create(companyClient, p1, agreement1, "employment_practice", "fact", practice, 400)
	practice["labor_contract_type"] = "other"
	practice["labor_contract_number"] = "ТД-42"
	practice["labor_contract_date"] = "2026-09-01"
	create(companyClient, p1, agreement1, "employment_practice", "fact", practice, 400)
	practice["labor_contract_type"] = "fixed_term"
	practice["practice_agreement_number"] = "ПР-12"
	practice["practice_agreement_date"] = "2026-05-15"
	createdPractice := object(create(companyClient, p1, agreement1, "employment_practice", "fact", practice, 201))
	if money(createdPractice["amount_rub"]) != "15170.00" {
		t.Fatal("wrong employment practice formula")
	}
	if _, err := db.Exec(`DELETE FROM entries WHERE id=$1`, createdPractice["id"]); err != nil {
		t.Fatalf("remove isolated employment practice fixture: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM agreement_activity_requirements WHERE agreement_id=$1 AND category_code='employment_practice'`, agreement1); err != nil {
		t.Fatalf("restore agreement after employment practice fixture: %v", err)
	}
	entries := call(partnerClient, "GET", "/entries?category_code=internship", nil, 200)
	if bytes.Contains(entries, []byte("Поддельное")) {
		t.Fatal("mentor snapshot forged")
	}
	internship["mentor_id"] = "wrong"
	create(companyClient, p1, agreement1, "internship", "fact", internship, 400)
	statusPath := "/obligations?partner_id=" + p1 + "&agreement_id=" + agreement1 + "&report_year=2026&period_type=plan"
	status := object(call(partnerClient, "GET", statusPath, nil, 200))
	if !status["required"].(bool) || !status["teachers"].(bool) || status["ood_rpd"].(bool) {
		t.Fatal("wrong obligations")
	}
	create(companyClient, p1, agreement1, "top_it", "plan", map[string]interface{}{"org_name": p1, "project_name": "ТОП ИТ", "program_name": "ИТ", "cofinancing_report_reference": "Отчёт 01", "cofinancing_amount_rub": 1000}, 201)
	status = object(call(partnerClient, "GET", statusPath, nil, 200))
	if !status["required"].(bool) || !status["top_it"].(bool) || status["top_it_exception"].(bool) {
		t.Fatal("TOP exemption was granted without an approved second educational organization")
	}
	otherAgreement := agreement("B-" + stamp)
	otherAgreement["partner_ids"] = []string{p2}
	otherAgreement["activity_codes"] = []string{"teachers", "ood_rpd", "internship", "minc_decision"}
	call(admin, "PUT", "/agreements/"+agreement2, otherAgreement, 200)
	mentor2 := object(call(admin, "POST", "/mentors", map[string]string{"partner_id": p2, "full_name": "Орлов Олег Олегович"}, 201))["id"].(string)
	create(admin, p2, agreement2, "teachers", "plan", teacher(p2), 201)
	create(admin, p2, agreement2, "ood_rpd", "plan", map[string]interface{}{"org_name": p2, "doc_type": "rpd", "level": "vo", "activity_type": "expertise", "program_name": "Другая программа"}, 201)
	create(admin, p2, agreement2, "internship", "plan", map[string]interface{}{"org_name": p2, "mentor_id": mentor2, "student_full_name": "Орлов Студент", "duration_months": 1, "student_load_hours_per_month": 2, "mentor_load_hours_per_month": 1, "mentor_assignment_start": "2026-01-01", "mentor_assignment_end": "2026-01-31", "mentor_order_number": "1-ОК", "mentor_order_date": "2025-12-30"}, 201)
	create(admin, p2, agreement2, "minc_decision", "plan", map[string]interface{}{
		"org_name": p2, "decision_reference": "Решение МЦ-1", "instruction_authority": "president",
		"instruction_type": "president_instruction", "instruction_reference": "Поручение П-1",
		"decision_number": "МЦ-1", "decision_date": "2026-01-01", "implementation_start": "2026-01-01",
		"implementation_deadline": "2026-12-31", "implementation_conditions": "Передать результат по акту",
		"activity_description": "Тестовое мероприятие", "metric_description": "Одна единица",
		"metric_unit": "ед.", "actual_volume": 1, "calculation_basis": "Фактическая стоимость", "amount_manual": 1000,
		"decision_required_documents": "Акт", "decision_provided_documents": "",
	}, 201)
	otherTransition := "/report-workflow/transition?agreement_id=" + agreement2 + "&report_year=2026&period_type=plan"
	call(admin, "POST", otherTransition, map[string]interface{}{"status": "ready", "scope_confirmed": true, "conditions_confirmed": true, "evidence_confirmed": true, "comment": "Другая ОО комплектна"}, 200)
	call(admin, "POST", otherTransition, map[string]interface{}{"status": "verified", "comment": "Проверена другая ОО"}, 200)
	call(admin, "POST", otherTransition, map[string]interface{}{"status": "approved", "comment": "Утверждена другая ОО"}, 200)
	status = object(call(partnerClient, "GET", statusPath, nil, 200))
	if status["required"].(bool) || !status["top_it_exception"].(bool) {
		t.Fatal("verified TOP exception across another educational organization was not applied")
	}
	beforeApprovalDashboard := object(call(partnerClient, "GET", "/dashboard?report_year=2026", nil, 200))
	if money(beforeApprovalDashboard["eligible_plan_total_rub"]) != "0.00" {
		t.Fatal("individual mandatory rows were counted before their agreement report was approved")
	}
	status = object(call(partnerClient, "GET", strings.ReplaceAll(statusPath, "period_type=plan", "period_type=fact"), nil, 200))
	if !status["required"].(bool) || status["top_it"].(bool) {
		t.Fatal("TOP leaked into fact")
	}
	status = object(call(partnerClient, "GET", strings.ReplaceAll(statusPath, "2026", "2027"), nil, 200))
	if !status["required"].(bool) {
		t.Fatal("TOP leaked into another year")
	}
	call(partnerClient, "GET", strings.ReplaceAll(statusPath, p1, p2), nil, 403)
	// Batch upload: optional, supported even on plan, and scoped on download.
	attachments := object(upload(companyClient, "/entries/"+id+"/attachments", map[string][]byte{"Акт 1.txt": []byte("one"), "Акт 2.txt": []byte("two")}, 201))
	uploadedFiles := attachments["files"].([]interface{})
	if len(uploadedFiles) != 2 {
		t.Fatal("batch not saved")
	}
	for _, raw := range uploadedFiles {
		if len(raw.(map[string]interface{})["content_sha256"].(string)) != 64 {
			t.Fatal("uploaded file has no SHA-256")
		}
	}
	attachID := uploadedFiles[0].(map[string]interface{})["id"].(string)
	expiringAttachID := uploadedFiles[1].(map[string]interface{})["id"].(string)
	call(partnerClient, "GET", "/attachments/"+attachID+"/download", nil, 200)
	var storedPath string
	if err := db.QueryRow(`SELECT storage_path FROM attachments WHERE id=$1`, attachID).Scan(&storedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storedPath, []byte("tampered"), 0640); err != nil {
		t.Fatal(err)
	}
	call(partnerClient, "GET", "/attachments/"+attachID+"/download", nil, 410)
	foreignAttach := object(upload(admin, "/entries/"+foreign+"/attachments", map[string][]byte{"private.txt": []byte("secret")}, 201))["id"].(string)
	call(partnerClient, "GET", "/attachments/"+foreignAttach+"/download", nil, 403)
	upload(companyClient, "/entries/"+id+"/attachments", map[string][]byte{"empty.txt": {}}, 400)
	db.Exec(`UPDATE attachments SET retention_expires_at=now()-interval '1 second' WHERE id=$1`, expiringAttachID)
	call(partnerClient, "GET", "/attachments/"+expiringAttachID+"/download", nil, 410)
	// Excel: preview doesn't insert; commit recalculates; same batch cannot duplicate.
	wb := xlsx.New()
	wb.AddSheet("Данные", []string{"course_name", "education_level", "semester", "teacher_full_name", "employment_form", "academic_hours", "it_experience_days", "okz_code", "employment_contract_reference", "appointment_order_reference", "individual_plan_reference", "class_schedule"}, [][]interface{}{{"Импорт", "bachelor", 1, "Петров Пётр", "ГПХ", 3, 365, "2512", "ГПХ-1", "Приказ-1", "План-1", "По расписанию"}})
	book, _ := wb.Bytes()
	importPath := "/entries/import?partner_id=" + p1 + "&agreement_id=" + agreement1 + "&category_code=teachers&period_type=fact&report_year=2026"
	upload(partnerClient, importPath, map[string][]byte{"data.xlsx": book}, 403)
	preview := object(upload(companyClient, importPath, map[string][]byte{"data.xlsx": book}, 200))
	if preview["committed"].(bool) || money(preview["total_rub"]) != "12420.00" {
		t.Fatal("bad preview")
	}
	committed := object(upload(companyClient, importPath+"&commit=1", map[string][]byte{"data.xlsx": book}, 201))
	if !committed["committed"].(bool) {
		t.Fatal("not committed")
	}
	upload(companyClient, importPath+"&commit=1", map[string][]byte{"data.xlsx": book}, 409)
	before := call(partnerClient, "GET", "/entries?category_code=teachers&period_type=fact", nil, 200)
	bad := xlsx.New()
	bad.AddSheet("Данные", []string{"course_name", "education_level", "semester", "teacher_full_name", "employment_form", "academic_hours"}, [][]interface{}{{"ok", "bachelor", 1, "Петров Пётр", "ГПХ", 3}, {"bad", "bachelor", 1, "Петров Пётр", "ГПХ", -1}})
	badBook, _ := bad.Bytes()
	badResult := object(upload(companyClient, importPath+"&commit=1", map[string][]byte{"bad.xlsx": badBook}, 200))
	if len(badResult["errors"].([]interface{})) != 1 {
		t.Fatal("invalid row accepted")
	}
	after := call(partnerClient, "GET", "/entries?category_code=teachers&period_type=fact", nil, 200)
	if !bytes.Equal(before, after) {
		t.Fatal("partial import happened")
	}
	workflowPath := "/report-workflow?agreement_id=" + agreement1 + "&report_year=2026&period_type=fact"
	workflow := object(call(partnerClient, "GET", workflowPath, nil, 200))
	if workflow["status"] != "draft" || workflow["can_mark_ready"].(bool) {
		t.Fatal("incomplete agreement report was considered ready")
	}
	transitionPath := "/report-workflow/transition?agreement_id=" + agreement1 + "&report_year=2026&period_type=fact"
	confirmations := map[string]interface{}{"status": "ready", "scope_confirmed": true, "conditions_confirmed": true, "evidence_confirmed": true, "counterparty_confirmed": true, "comment": "Комплект проверен"}
	call(companyClient, "POST", transitionPath, confirmations, 422)
	call(partnerClient, "GET", "/reports/export?report_year=2026&period_type=fact", nil, 409)
	create(companyClient, p1, agreement1, "ood_rpd", "fact", map[string]interface{}{
		"org_name": p1, "doc_type": "rpd", "level": "vo", "activity_type": "expertise", "program_name": "Безопасность",
		"expert_full_name": "Эксперт Эксперт", "project_document_reference": "Проект-1",
		"expert_conclusion_reference": "Заключение-1", "approval_reference": "Протокол-1",
	}, 201)
	create(companyClient, p1, agreement1, "top_it", "fact", map[string]interface{}{
		"org_name": p1, "project_name": "ТОП ИТ", "program_name": "ИТ", "cofinancing_report_reference": "Отчёт факт", "cofinancing_amount_rub": 1000,
		"planned_cofinancing_amount_rub": 1000, "transferred_amount_rub": 1000, "actual_spent_amount_rub": 1000,
		"top_agreement_reference": "Договор-1", "payment_order_reference": "Платёж-1", "spending_act_reference": "Акт-1", "ano_letter_reference": "Письмо-1",
	}, 201)
	call(companyClient, "POST", transitionPath, confirmations, 200)
	workflow = object(call(companyClient, "GET", workflowPath, nil, 200))
	if workflow["counterparty_confirmed"].(bool) {
		t.Fatal("the IT organization confirmed its own counterparty review")
	}
	call(companyClient, "POST", transitionPath, map[string]interface{}{"status": "verified", "comment": "Попытка самопроверки"}, 409)
	call(partnerClient, "POST", transitionPath, map[string]interface{}{"status": "verified", "comment": "Перечень рассмотрен и согласован"}, 200)
	call(admin, "POST", transitionPath, map[string]interface{}{"status": "approved", "comment": "Утверждено администратором"}, 200)
	report := call(partnerClient, "GET", "/reports/export?report_year=2026&period_type=fact", nil, 200)
	reportRows, e := xlsx.ReadFirst(report)
	if e != nil {
		t.Fatal(e)
	}
	for _, row := range reportRows {
		if strings.Contains(strings.Join(row, " "), "Тестовый вуз B "+stamp) {
			t.Fatal("report leaks foreign partner")
		}
	}
	call(partnerClient, "GET", "/reports/export?report_year=2026&period_type=fact&format=docx", nil, 200)
	dash := object(call(partnerClient, "GET", "/dashboard?report_year=2026", nil, 200))
	if money(dash["fact_total_rub"]) != "98760.00" || money(dash["eligible_fact_total_rub"]) != "98760.00" {
		t.Fatalf("dashboard scope/formulas wrong: %v", dash)
	}
	internship["mentor_id"] = mentor
	call(companyClient, "PUT", "/entries/"+traineeID, map[string]interface{}{"payload": internship, "comment": "Уточнение данных после утверждения"}, 200)
	call(partnerClient, "GET", "/reports/export?report_year=2026&period_type=fact", nil, 409)
	dash = object(call(partnerClient, "GET", "/dashboard?report_year=2026", nil, 200))
	if money(dash["eligible_fact_total_rub"]) != "0.00" {
		t.Fatal("changed approved report was not returned to draft")
	}
	directoryBook := xlsx.New()
	directoryBook.AddSheet("Данные", []string{"name", "partner_kind", "region", "inn", "ogrn", "license_number", "license_status", "institution_status", "registry_record_id", "source_url", "registry_updated_at"}, [][]interface{}{{"Колледж для импорта " + stamp, "kolledj", "Тестовый регион", "7736050003", "1027700070518", "Л035-ТЕСТ-3", "active", "active", "test-c-" + stamp, "https://islod.obrnadzor.gov.ru/rlic/details/test-3", time.Now().Format("2006-01-02")}})
	directoryData, _ := directoryBook.Bytes()
	previewDirectory := object(upload(admin, "/admin/directory-import", map[string][]byte{"directory.xlsx": directoryData}, 200))
	if previewDirectory["committed"].(bool) {
		t.Fatal("directory preview wrote data")
	}
	loadedDirectory := object(upload(admin, "/admin/directory-import?commit=1", map[string][]byte{"directory.xlsx": directoryData}, 200))
	if !loadedDirectory["committed"].(bool) {
		t.Fatal("directory commit failed")
	}
	filtered := call(admin, "GET", "/directory?partner_kind=kolledj&q="+stamp, nil, 200)
	if !bytes.Contains(filtered, []byte("Колледж для импорта")) {
		t.Fatal("directory search failed")
	}
	filtered = call(admin, "GET", "/directory?partner_kind=school&q=Колледж+для+импорта+"+stamp, nil, 200)
	if string(bytes.TrimSpace(filtered)) != "[]" {
		t.Fatal("directory type filter failed")
	}
	// Education users may preview the directory, but only an organization admin may commit it.
	upload(partnerClient, "/admin/directory-import", map[string][]byte{"directory.xlsx": directoryData}, 200)
	upload(partnerClient, "/admin/directory-import?commit=1", map[string][]byte{"directory.xlsx": directoryData}, 403)
	// MFA enrolment revokes old sessions; recovery codes are single-use.
	setup := object(call(admin, "POST", "/auth/mfa/enroll", map[string]string{"password": password}, 200))
	code, err := auth.TOTP(setup["secret"].(string), time.Now().Unix()/30)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := object(call(admin, "POST", "/auth/mfa/confirm", map[string]string{"code": code}, 200))
	recovery := confirmed["recovery_codes"].([]interface{})
	call(admin, "GET", "/auth/me", nil, 401)
	call(admin, "POST", "/auth/login", map[string]string{"email": email, "password": password, "code": code}, 401)
	call(admin, "POST", "/auth/login", map[string]string{"email": email, "password": password, "code": recovery[0].(string)}, 200)
	call(newClient(), "POST", "/auth/login", map[string]string{"email": email, "password": password, "code": recovery[0].(string)}, 401)
	call(admin, "POST", "/auth/password", map[string]string{"current_password": password, "new_password": "UpdatedPassword2!"}, 200)
	call(admin, "GET", "/auth/me", nil, 401)
	call(admin, "POST", "/auth/login", map[string]string{"email": email, "password": "UpdatedPassword2!", "code": recovery[1].(string)}, 200)
	for _, cookie := range admin.Jar.Cookies(mustURL(t, server.URL)) {
		if cookie.Name == auth.CookieName {
			var raw int
			if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE token=$1`, cookie.Value).Scan(&raw); err != nil || raw != 0 {
				t.Fatal("bearer token stored in plaintext", err)
			}
		}
	}
	t.Run("atomic authentication and session rollback", func(t *testing.T) { checkAtomicAuthentication(t, db) })
	t.Log("ACL, formulas, mentors, optional batch files, expiry, Excel atomicity/idempotency and exports verified")
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
