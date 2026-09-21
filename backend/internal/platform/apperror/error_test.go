package apperror

import (
	"errors"
	"testing"
)

func TestNewCopiesFieldsAndPreservesCause(t *testing.T) {
	cause := errors.New("database is offline")
	fields := map[string]string{"report_year": "out of range"}
	err := New(KindValidation, "invalid_report", "некорректный отчёт", fields, cause)
	fields["report_year"] = "changed"

	if err.Kind != KindValidation || err.Code != "invalid_report" || err.Fields["report_year"] != "out of range" {
		t.Fatalf("unexpected application error: %#v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("application error must preserve its cause")
	}
}
