package tests

import (
	"database/sql"
	"testing"

	"cybercalc/internal/testfixtures"
)

func TestSharedFixturesAreAvailableToExternalTests(t *testing.T) {
	var db *sql.DB
	factory := testfixtures.New(db, t.Name())
	if factory == nil || testfixtures.DefaultPassword == "" {
		t.Fatal("shared test fixture contract is unavailable")
	}
}
