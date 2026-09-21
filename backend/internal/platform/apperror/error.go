// Package apperror defines errors that may cross the service/HTTP boundary.
// Domain and repository packages should return their own errors; a service
// translates them to Error before exposing the failure to a transport.
package apperror

import "fmt"

// Kind describes the transport-independent class of an application error.
type Kind string

const (
	KindValidation   Kind = "validation"
	KindUnauthorized Kind = "unauthorized"
	KindForbidden    Kind = "forbidden"
	KindNotFound     Kind = "not_found"
	KindConflict     Kind = "conflict"
	KindUnavailable  Kind = "unavailable"
	KindInternal     Kind = "internal"
)

// Error carries a stable machine code and a safe user-facing message.
// Cause is kept for errors.Is/errors.As and logging, but is never serialized.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  map[string]string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs an application error. Fields are copied so a caller cannot
// mutate the error after it has crossed a package boundary.
func New(kind Kind, code, message string, fields map[string]string, cause error) *Error {
	copiedFields := make(map[string]string, len(fields))
	for field, problem := range fields {
		copiedFields[field] = problem
	}
	return &Error{Kind: kind, Code: code, Message: message, Fields: copiedFields, Cause: cause}
}
