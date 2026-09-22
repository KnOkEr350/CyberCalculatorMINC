package handlers

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestRegulatoryHeadersGolden(t *testing.T) {
	actual, err := json.Marshal(regulatoryHeaders)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/regulatory_headers.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, bytes.TrimSpace(expected)) {
		t.Fatalf("regulatory report headers differ from golden file\nactual: %s", actual)
	}
}
