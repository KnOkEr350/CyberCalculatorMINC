package testfixtures

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"cybercalc/internal/dbx"
	_ "github.com/lib/pq"
)

func TestCreateScenario(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN not set")
	}
	if !strings.Contains(dsn, "dbname=workspace_test") {
		t.Fatal("shared fixtures require an isolated workspace_test database")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := dbx.RunMigrations(db, os.Getenv("TEST_MIGRATIONS_DIR")); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	scenario, err := NewTx(tx, t.Name()).CreateScenario(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for label, id := range map[string]string{
		"company": scenario.Company.ID, "university": scenario.UniversityPartner.ID,
		"college": scenario.CollegePartner.ID, "school": scenario.SchoolPartner.ID,
		"authority": scenario.RegionalAuthority.ID, "university agreement": scenario.UniversityAgreement.ID,
		"college agreement": scenario.CollegeAgreement.ID, "school agreement": scenario.SchoolAgreement.ID,
	} {
		if id == "" {
			t.Fatalf("%s fixture has no ID", label)
		}
	}
	var partners, agreements, users int
	if err := tx.QueryRow(`SELECT
		(SELECT count(*) FROM partners WHERE it_company_id=$1),
		(SELECT count(*) FROM agreements WHERE it_company_id=$1),
		(SELECT count(*) FROM users WHERE it_company_id=$1 OR partner_id IN ($2,$3,$4))`,
		scenario.Company.ID, scenario.UniversityPartner.ID, scenario.CollegePartner.ID, scenario.SchoolPartner.ID).
		Scan(&partners, &agreements, &users); err != nil {
		t.Fatal(err)
	}
	if partners != 3 || agreements != 3 || users != 5 {
		t.Fatalf("scenario counts: partners=%d agreements=%d users=%d", partners, agreements, users)
	}
}
