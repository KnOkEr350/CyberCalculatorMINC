package service

import (
	"context"
	"errors"
	"testing"

	"cybercalc/internal/platform/apperror"
)

type probeStub struct {
	err error
}

func (p probeStub) Ping(context.Context) error { return p.err }

func TestLivenessDoesNotCallInfrastructure(t *testing.T) {
	if got := New(nil).Liveness().Status; got != "ok" {
		t.Fatalf("got %q, want ok", got)
	}
}

func TestReadiness(t *testing.T) {
	t.Run("ready", func(t *testing.T) {
		status, err := New(probeStub{}).Readiness(context.Background())
		if err != nil || status.Status != "ok" {
			t.Fatalf("status=%#v err=%v", status, err)
		}
	})

	t.Run("unavailable", func(t *testing.T) {
		cause := errors.New("offline")
		_, err := New(probeStub{err: cause}).Readiness(context.Background())
		var applicationError *apperror.Error
		if !errors.As(err, &applicationError) || applicationError.Kind != apperror.KindUnavailable || !errors.Is(err, cause) {
			t.Fatalf("unexpected error: %#v", err)
		}
	})
}
