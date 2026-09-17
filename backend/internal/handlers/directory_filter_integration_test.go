package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/dbx"
	"cybercalc/internal/middleware"
)

func TestDirectoryPaginationKeepsExactProgramFilter(t *testing.T) {
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
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	prefix := "Pagination-" + time.Now().Format("150405.000000000")
	_, err = db.Exec(`INSERT INTO education_directory(name,partner_kind,region,source,program_codes,listed_in_mincifry_order_27)
		SELECT $1||n,'vuz','Тестовый регион','test',ARRAY['09.03.01','38.03.01'],TRUE FROM generate_series(1,501) n`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO education_directory(name,partner_kind,region,source,program_codes,listed_in_mincifry_order_27)
		VALUES($1||'excluded','vuz','Тестовый регион','test',ARRAY['38.03.01'],TRUE),
		($1||'unknown','vuz','Тестовый регион','test','{}',TRUE),
		($1||'not-listed','vuz','Тестовый регион','test',ARRAY['09.03.01'],FALSE)`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE education_directory SET verification_status='verified',license_status='active',
		institution_status='active',verified_at=now(),registry_updated_at=CURRENT_DATE
		WHERE name=$1||'not-listed'`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	h := PartnerHandlers{DB: db}
	u := middleware.AuthUser{Role: "admin", EntityType: "organization"}
	for _, page := range []struct {
		offset, next string
		size         int
	}{{"0", "500", 500}, {"500", "", 1}} {
		w := httptest.NewRecorder()
		h.Directory(w, httptest.NewRequest("GET", "/directory?partner_kind=vuz&q="+prefix+"&offset="+page.offset, nil), u)
		var items []struct {
			Name          string   `json:"name"`
			MatchingCodes []string `json:"matching_program_codes"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || len(items) != page.size || w.Header().Get("X-Next-Offset") != page.next {
			t.Fatalf("pagination: status=%d rows=%d body=%s", w.Code, len(items), w.Body.String())
		}
		for _, item := range items {
			if strings.HasSuffix(item.Name, "unknown") || strings.HasSuffix(item.Name, "excluded") || len(item.MatchingCodes) != 1 || item.MatchingCodes[0] != "09.03.01" {
				t.Fatalf("incorrect program filter: %+v", item)
			}
		}
	}
	w := httptest.NewRecorder()
	h.Directory(w, httptest.NewRequest("GET", "/directory?partner_kind=vuz&review_all=1&q="+prefix+"not-listed", nil), u)
	var reviewItems []struct {
		Name       string `json:"name"`
		Selectable bool   `json:"selectable"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reviewItems); err != nil || w.Code != 200 || len(reviewItems) != 1 {
		t.Fatalf("review list: status=%d rows=%d body=%s", w.Code, len(reviewItems), w.Body.String())
	}
	if reviewItems[0].Selectable {
		t.Fatal("a university outside Order 27 must never be selectable")
	}
}
