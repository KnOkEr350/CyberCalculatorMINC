package repository

import (
	"context"
	"errors"
	"testing"
)

func TestSQLProbeRejectsMissingDatabase(t *testing.T) {
	err := NewSQLProbe(nil).Ping(context.Background())
	if !errors.Is(err, ErrDatabaseNotConfigured) {
		t.Fatalf("got %v, want ErrDatabaseNotConfigured", err)
	}
}
