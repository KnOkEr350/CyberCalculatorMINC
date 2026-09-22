// Package audit defines the application-wide contract for audit events.
// Domain modules use this package instead of depending on the audit_log schema.
package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	ActorUser   = "user"
	ActorSystem = "system"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type Actor struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

type Entity struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
}

// Event is the canonical audit contract shared by all modules. Before and
// After contain snapshots, not patches, so an event remains understandable
// without replaying earlier records.
type Event struct {
	Actor     Actor  `json:"actor"`
	Action    string `json:"action"`
	Entity    Entity `json:"entity"`
	Before    any    `json:"old,omitempty"`
	After     any    `json:"new,omitempty"`
	RequestID string `json:"request_id"`
	Comment   string `json:"comment,omitempty"`
}

type Execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, strings.TrimSpace(requestID))
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func NewRequestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate audit request id: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func UserActor(id string) Actor {
	id = strings.TrimSpace(id)
	if id == "" {
		return Actor{Type: ActorSystem}
	}
	return Actor{Type: ActorUser, ID: id}
}

func (event *Event) normalize(ctx context.Context) error {
	event.Actor.Type = strings.TrimSpace(event.Actor.Type)
	event.Actor.ID = strings.TrimSpace(event.Actor.ID)
	event.Action = strings.TrimSpace(event.Action)
	event.Entity.Type = strings.TrimSpace(event.Entity.Type)
	event.Entity.ID = strings.TrimSpace(event.Entity.ID)
	event.RequestID = strings.TrimSpace(event.RequestID)
	event.Comment = strings.TrimSpace(event.Comment)

	if event.RequestID == "" {
		event.RequestID = RequestIDFromContext(ctx)
	}
	if event.RequestID == "" {
		requestID, err := NewRequestID()
		if err != nil {
			return err
		}
		event.RequestID = requestID
	}
	return nil
}

func (event Event) Validate() error {
	if event.Actor.Type != ActorUser && event.Actor.Type != ActorSystem {
		return errors.New("audit actor type must be user or system")
	}
	if event.Actor.Type == ActorUser && event.Actor.ID == "" {
		return errors.New("audit user actor requires an id")
	}
	if event.Actor.Type == ActorSystem && event.Actor.ID != "" {
		return errors.New("audit system actor must not have a user id")
	}
	if event.Action == "" {
		return errors.New("audit action is required")
	}
	if !identifierPattern.MatchString(event.Action) {
		return errors.New("audit action must be a lower_snake_case identifier")
	}
	if event.Entity.Type == "" {
		return errors.New("audit entity type is required")
	}
	if !identifierPattern.MatchString(event.Entity.Type) {
		return errors.New("audit entity type must be a lower_snake_case identifier")
	}
	if event.RequestID == "" {
		return errors.New("audit request id is required")
	}
	if len(event.RequestID) > 128 {
		return errors.New("audit request id must not exceed 128 bytes")
	}
	return nil
}

// Write persists the event using the legacy columns while enforcing the
// canonical actor/action/entity/old/new/request-id contract at one boundary.
func Write(ctx context.Context, db Execer, event Event) error {
	if db == nil {
		return errors.New("audit database is required")
	}
	if err := event.normalize(ctx); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}

	oldJSON, err := marshalNullable(event.Before)
	if err != nil {
		return fmt.Errorf("marshal audit old value: %w", err)
	}
	newJSON, err := marshalNullable(event.After)
	if err != nil {
		return fmt.Errorf("marshal audit new value: %w", err)
	}

	_, err = db.ExecContext(ctx, `INSERT INTO audit_log
		(actor_type,user_id,action,entity_type,entity_id,old_value,new_value,request_id,comment_text)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		event.Actor.Type, nullableString(event.Actor.ID), event.Action,
		event.Entity.Type, nullableString(event.Entity.ID), oldJSON, newJSON,
		event.RequestID, nullableString(event.Comment),
	)
	return err
}

func marshalNullable(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
