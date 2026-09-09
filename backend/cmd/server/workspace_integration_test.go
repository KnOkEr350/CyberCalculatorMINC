package main

import (
	"bytes"
	"cybercalc/internal/auth"
	"cybercalc/internal/config"
	"cybercalc/internal/dbx"
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
	cfg := config.Config{UploadDir: t.TempDir(), MFAKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	server := httptest.NewServer(buildRoutes(db, cfg))
	defer server.Close()
	stamp := fmt.Sprint(time.Now().UnixNano())
	password := "WorkspaceTest1!"
	hash, _ := auth.HashPassword(password)
	email := "test" + stamp + "@workspace.test"
	if _, e := db.Exec(`INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES($1,$2,'Тест Администратор','admin','organization')`, email, hash); e != nil {
		t.Fatal(e)
	}
	newClient := func() *http.Client { jar, _ := cookiejar.New(nil); return &http.Client{Jar: jar} }
	admin, partnerClient := newClient(), newClient()
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
	object := func(b []byte) map[string]interface{} {
		t.Helper()
		var m map[string]interface{}
		if e := json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	call(admin, "POST", "/auth/login", map[string]string{"email": email, "password": password}, 200)
	call(admin, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "target_amount_rub": 5000}, 200)
	call(admin, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "target_amount_rub": 6000}, 200)
	adminDashboard := object(call(admin, "GET", "/dashboard?report_year=2026", nil, 200))
	if adminDashboard["target_amount_rub"].(float64) != 6000 {
		t.Fatal("owner-scoped target upsert failed")
	}
	p1 := object(call(admin, "POST", "/partners", map[string]string{"name": "Тестовый вуз A " + stamp, "partner_kind": "vuz", "agreement_date": "2026-01-01"}, 201))["id"].(string)
	p2 := object(call(admin, "POST", "/partners", map[string]string{"name": "Тестовый вуз B " + stamp, "partner_kind": "vuz"}, 201))["id"].(string)
	partnerEmail := "partner" + stamp + "@workspace.test"
	call(admin, "POST", "/admin/users", map[string]interface{}{"email": partnerEmail, "password": password, "full_name": "Представитель Вуза", "role": "user", "entity_type": "edu_institution", "partner_id": p1}, 201)
	call(partnerClient, "POST", "/auth/login", map[string]string{"email": partnerEmail, "password": password}, 200)
	call(partnerClient, "POST", "/auth/entity-type", map[string]string{"entity_type": "organization"}, 403)
	call(partnerClient, "POST", "/partners", map[string]string{"name": "Чужой вуз", "partner_kind": "vuz"}, 403)
	call(partnerClient, "POST", "/dashboard/target", map[string]interface{}{"report_year": 2026, "target_amount_rub": 1000}, 403)
	partners := call(partnerClient, "GET", "/partners", nil, 200)
	if !bytes.Contains(partners, []byte(p1)) || bytes.Contains(partners, []byte(p2)) {
		t.Fatal("partner list leaks data")
	}
	mentor := object(call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p1, "full_name": "Иванов Иван Иванович"}, 201))["id"].(string)
	call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p2, "full_name": "Иванов Иван Иванович"}, 403)
	call(partnerClient, "POST", "/mentors", map[string]string{"partner_id": p1, "full_name": "123"}, 400)
	teacher := func(p string) map[string]interface{} {
		return map[string]interface{}{"org_name": p, "course_name": "ИТ", "teacher_full_name": "Петров Пётр", "employment_form": "ГПХ", "academic_hours": 2}
	}
	create := func(client *http.Client, p, category, period string, payload map[string]interface{}, want int) []byte {
		return call(client, "POST", "/entries", map[string]interface{}{"partner_id": p, "category_code": category, "period_type": period, "report_year": 2026, "audience": "vuz", "payload": payload}, want)
	}
	first := object(create(partnerClient, p1, "teachers", "plan", teacher(p1), 201))
	id := first["id"].(string)
	if first["amount_rub"].(float64) != 8280 {
		t.Fatal("wrong teacher formula")
	}
	foreign := object(create(admin, p2, "teachers", "fact", teacher(p2), 201))["id"].(string)
	shared := object(create(admin, p1, "teachers", "plan", teacher(p1), 201))["id"].(string)
	create(partnerClient, p2, "teachers", "plan", teacher(p2), 403)
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
	call(partnerClient, "GET", "/entries?report_year=oops", nil, 400)
	internship := map[string]interface{}{"org_name": p1, "mentor_id": mentor, "mentor_full_name": "Поддельное Имя", "student_full_name": "Сидоров Сидор", "duration_months": 2, "student_load_hours_per_month": 10, "mentor_load_hours_per_month": 3}
	trainee := object(create(partnerClient, p1, "internship", "fact", internship, 201))
	if trainee["amount_rub"].(float64) != 30340 {
		t.Fatal("wrong internship formula")
	}
	entries := call(partnerClient, "GET", "/entries?category_code=internship", nil, 200)
	if bytes.Contains(entries, []byte("Поддельное")) {
		t.Fatal("mentor snapshot forged")
	}
	internship["mentor_id"] = "wrong"
	create(partnerClient, p1, "internship", "fact", internship, 400)
	statusPath := "/obligations?partner_id=" + p1 + "&report_year=2026&period_type=plan"
	status := object(call(partnerClient, "GET", statusPath, nil, 200))
	if !status["required"].(bool) || !status["teachers"].(bool) || status["ood_rpd"].(bool) {
		t.Fatal("wrong obligations")
	}
	create(partnerClient, p1, "top_it", "plan", map[string]interface{}{"org_name": p1, "project_name": "ТОП ИТ", "program_name": "ИТ", "cofinancing_report_reference": "Отчёт 01", "cofinancing_amount_rub": 1000}, 201)
	status = object(call(partnerClient, "GET", statusPath, nil, 200))
	if status["required"].(bool) || !status["top_it"].(bool) {
		t.Fatal("TOP exemption missing")
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
	attachments := object(upload(partnerClient, "/entries/"+id+"/attachments", map[string][]byte{"Акт 1.txt": []byte("one"), "Акт 2.txt": []byte("two")}, 201))
	if len(attachments["files"].([]interface{})) != 2 {
		t.Fatal("batch not saved")
	}
	attachID := attachments["id"].(string)
	call(partnerClient, "GET", "/attachments/"+attachID+"/download", nil, 200)
	foreignAttach := object(upload(admin, "/entries/"+foreign+"/attachments", map[string][]byte{"private.txt": []byte("secret")}, 201))["id"].(string)
	call(partnerClient, "GET", "/attachments/"+foreignAttach+"/download", nil, 403)
	upload(partnerClient, "/entries/"+id+"/attachments", map[string][]byte{"empty.txt": {}}, 400)
	db.Exec(`UPDATE attachments SET retention_expires_at=now()-interval '1 second' WHERE id=$1`, attachID)
	call(partnerClient, "GET", "/attachments/"+attachID+"/download", nil, 410)
	// Excel: preview doesn't insert; commit recalculates; same batch cannot duplicate.
	wb := xlsx.New()
	wb.AddSheet("Данные", []string{"course_name", "teacher_full_name", "employment_form", "academic_hours"}, [][]interface{}{{"Импорт", "Петров Пётр", "ГПХ", 3}})
	book, _ := wb.Bytes()
	importPath := "/entries/import?partner_id=" + p1 + "&category_code=teachers&period_type=fact&report_year=2026"
	preview := object(upload(partnerClient, importPath, map[string][]byte{"data.xlsx": book}, 200))
	if preview["committed"].(bool) || preview["total_rub"].(float64) != 12420 {
		t.Fatal("bad preview")
	}
	committed := object(upload(partnerClient, importPath+"&commit=1", map[string][]byte{"data.xlsx": book}, 201))
	if !committed["committed"].(bool) {
		t.Fatal("not committed")
	}
	upload(partnerClient, importPath+"&commit=1", map[string][]byte{"data.xlsx": book}, 409)
	before := call(partnerClient, "GET", "/entries?category_code=teachers&period_type=fact", nil, 200)
	bad := xlsx.New()
	bad.AddSheet("Данные", []string{"course_name", "teacher_full_name", "employment_form", "academic_hours"}, [][]interface{}{{"ok", "Петров Пётр", "ГПХ", 3}, {"bad", "Петров Пётр", "ГПХ", -1}})
	badBook, _ := bad.Bytes()
	badResult := object(upload(partnerClient, importPath+"&commit=1", map[string][]byte{"bad.xlsx": badBook}, 200))
	if len(badResult["errors"].([]interface{})) != 1 {
		t.Fatal("invalid row accepted")
	}
	after := call(partnerClient, "GET", "/entries?category_code=teachers&period_type=fact", nil, 200)
	if !bytes.Equal(before, after) {
		t.Fatal("partial import happened")
	}
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
	if dash["fact_total_rub"].(float64) != 42760 {
		t.Fatalf("dashboard scope/formulas wrong: %v", dash)
	}
	directoryBook := xlsx.New()
	directoryBook.AddSheet("Данные", []string{"name", "partner_kind", "region", "source"}, [][]interface{}{{"Колледж для импорта " + stamp, "kolledj", "Тестовый регион", "Тестовый набор 2026"}})
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
	filtered = call(admin, "GET", "/directory?partner_kind=school&q="+stamp, nil, 200)
	if string(bytes.TrimSpace(filtered)) != "[]" {
		t.Fatal("directory type filter failed")
	}
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
