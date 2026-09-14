package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cybercalc/internal/dbx"
	"cybercalc/internal/middleware"
	"cybercalc/internal/models"
)

func TestITCompanyBulkImportAndPagination(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("isolated workspace_test database required")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	var userID string
	err = db.QueryRow(`INSERT INTO users(email,password_hash,full_name,role,entity_type) VALUES('it-registry@workspace.test','test-only','Тест импорта ИТ','admin','edu_institution') ON CONFLICT(email) DO UPDATE SET full_name=EXCLUDED.full_name RETURNING id::text`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	data := testITRegistry(t, 650, ',')
	for attempt := 0; attempt < 2; attempt++ {
		if count, err := ImportITCompanies(context.Background(), db, data, userID); err != nil || count != 650 {
			t.Fatalf("import: count=%d err=%v", count, err)
		}
	}
	user := middleware.AuthUser{ID: userID, Role: models.RoleAdmin, EntityType: models.EntityEduInst}
	h := ITCompanyHandlers{DB: db}
	for _, page := range []struct {
		offset string
		size   int
		next   string
	}{{"0", 500, "500"}, {"500", 150, ""}} {
		w := httptest.NewRecorder()
		h.List(w, httptest.NewRequest("GET", "/it-companies?q=Компания&offset="+page.offset, nil), user)
		var items []models.ITCompany
		if err = json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || len(items) != page.size || w.Header().Get("X-Next-Offset") != page.next {
			t.Fatalf("pagination %s: %d rows=%d %s", page.offset, w.Code, len(items), w.Body.String())
		}
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\ufeff")))
	rows, _ := reader.ReadAll()
	rows[1][0] = "Изменение должно откатиться"
	rows[len(rows)-1][2] = "1027700132195" // valid checksum, different existing legal entity
	var corrupted bytes.Buffer
	w := csv.NewWriter(&corrupted)
	w.WriteAll(rows)
	w.Flush()
	if _, err = ImportITCompanies(context.Background(), db, corrupted.Bytes(), userID); err == nil {
		t.Fatal("identity conflict accepted")
	}
	var name string
	if err = db.QueryRow(`SELECT name FROM accredited_it_companies WHERE inn=$1`, rows[1][1]).Scan(&name); err != nil || name == rows[1][0] {
		t.Fatal("failed import committed a partial update", err)
	}
}
