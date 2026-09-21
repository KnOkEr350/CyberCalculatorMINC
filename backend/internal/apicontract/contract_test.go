package apicontract

import (
	"path/filepath"
	"testing"
)

func TestOpenAPIV1IsValidAndMatchesRegisteredRoutes(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "api", "openapi", "v1.json")
	document, err := Load(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(document); err != nil {
		t.Fatal(err)
	}
	registered, err := DiscoverModuleRoutes(filepath.Join("..", "modules"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CompareRoutes(Operations(document), registered); err != nil {
		t.Fatal(err)
	}
	if got := len(registered); got != 65 {
		t.Fatalf("route inventory changed unexpectedly: got %d; update the contract and this assertion", got)
	}
}

func TestCompareRoutesReportsBothDirections(t *testing.T) {
	err := CompareRoutes(
		[]Operation{{Method: "GET", Path: "/api/stale"}},
		[]Operation{{Method: "POST", Path: "/api/new"}},
	)
	if err == nil {
		t.Fatal("expected mismatch")
	}
}
