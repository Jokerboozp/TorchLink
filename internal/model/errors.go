package model

import "errors"

// Error categories decide how a domain error is answered. A domain error
// belongs to one category (built with Invalid, Conflict, Forbidden or
// Unavailable, or wrapping a category with %w), and the HTTP layer maps the
// category instead of knowing every domain's errors. ErrNotFound is the
// not-found category.
var (
	// ErrInvalid marks a request whose content breaks a business rule (422).
	ErrInvalid = errors.New("invalid request")
	// ErrConflict marks a change that conflicts with the current state, such as
	// a stale version or a duplicate (409).
	ErrConflict = errors.New("conflict with the current state")
	// ErrForbidden marks an operation the caller may not perform (403).
	ErrForbidden = errors.New("operation not allowed")
	// ErrUnavailable marks a temporarily unavailable dependency (503).
	ErrUnavailable = errors.New("temporarily unavailable")
)

// Error is a domain error with a message for the caller and a category.
type Error struct {
	Category error
	Message  string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Category }

// Invalid is an ErrInvalid error with message.
func Invalid(message string) *Error { return &Error{Category: ErrInvalid, Message: message} }

// Conflict is an ErrConflict error with message.
func Conflict(message string) *Error { return &Error{Category: ErrConflict, Message: message} }

// Unavailable is an ErrUnavailable error with message.
func Unavailable(message string) *Error { return &Error{Category: ErrUnavailable, Message: message} }
