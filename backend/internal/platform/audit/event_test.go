package audit

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type captureExecer struct {
	query string
	args  []any
	err   error
}

func (db *captureExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	db.query = query
	db.args = args
	return nil, db.err
}

func TestWriteUsesCanonicalContractAndContextRequestID(t *testing.T) {
	db := &captureExecer{}
	ctx := WithRequestID(context.Background(), "request-123")
	event := Event{
		Actor:  UserActor("user-id"),
		Action: " update ",
		Entity: Entity{Type: " entry ", ID: "entry-id"},
		Before: map[string]int{"amount": 10},
		After:  map[string]int{"amount": 20},
	}
	if err := Write(ctx, db, event); err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 9 {
		t.Fatalf("expected 9 audit arguments, got %d", len(db.args))
	}
	if db.args[0] != ActorUser || db.args[1] != "user-id" || db.args[2] != "update" {
		t.Fatalf("unexpected actor/action arguments: %#v", db.args[:3])
	}
	if db.args[3] != "entry" || db.args[4] != "entry-id" || db.args[7] != "request-123" {
		t.Fatalf("unexpected entity/request arguments: %#v", db.args)
	}
	if string(db.args[5].([]byte)) != `{"amount":10}` || string(db.args[6].([]byte)) != `{"amount":20}` {
		t.Fatalf("unexpected snapshots: %#v", db.args)
	}
}

func TestWriteRejectsInvalidEventBeforeDatabase(t *testing.T) {
	db := &captureExecer{}
	err := Write(context.Background(), db, Event{Actor: Actor{Type: ActorUser}, Action: "create", Entity: Entity{Type: "entry"}})
	if err == nil || db.query != "" {
		t.Fatalf("invalid actor must be rejected before insert, err=%v", err)
	}
}

func TestWriteRejectsUnstableActionIdentifier(t *testing.T) {
	db := &captureExecer{}
	err := Write(context.Background(), db, Event{Actor: Actor{Type: ActorSystem}, Action: "Human readable action", Entity: Entity{Type: "entry"}})
	if err == nil || db.query != "" {
		t.Fatalf("unstable action identifier must be rejected before insert, err=%v", err)
	}
}

func TestWriteReturnsDatabaseError(t *testing.T) {
	want := errors.New("database unavailable")
	db := &captureExecer{err: want}
	err := Write(context.Background(), db, Event{Actor: Actor{Type: ActorSystem}, Action: "sync", Entity: Entity{Type: "directory"}})
	if !errors.Is(err, want) {
		t.Fatalf("expected database error, got %v", err)
	}
	if db.args[7] == "" {
		t.Fatal("background audit event must receive a generated request id")
	}
}
