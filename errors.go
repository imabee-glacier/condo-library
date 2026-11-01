package condo

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error code. It never contains a
// credential, token, cookie, or personal value.
type Code string

const (
	// ErrCodeUnauthenticated is returned when the portal requires sign-in
	// and no valid session or usable credential is available.
	ErrCodeUnauthenticated Code = "unauthenticated"
	// ErrCodeNotFound is returned when a requested page or item is absent.
	ErrCodeNotFound Code = "not_found"
	// ErrCodeNotAvailable is returned for a feature that is not in the
	// resident menu for this unit, or that has no mapped endpoint.
	ErrCodeNotAvailable Code = "not_available"
	// ErrCodeSubmissionUndiscovered is returned when an action's preview is
	// valid and confirmed, but its underlying submission endpoint has not
	// been discovered. No request is sent in this case.
	ErrCodeSubmissionUndiscovered Code = "submission_endpoint_undiscovered"
	// ErrCodeConfirmationRequired is returned when a consequential action
	// is called without explicit confirmation.
	ErrCodeConfirmationRequired Code = "confirmation_required"
	// ErrCodeValidation is returned for invalid caller input.
	ErrCodeValidation Code = "validation"
	// ErrCodeHTTP is returned for non-2xx portal responses.
	ErrCodeHTTP Code = "http"
	// ErrCodeParse is returned when a portal page cannot be parsed.
	ErrCodeParse Code = "parse"
)

// Error is the SDK's structured error type.
type Error struct {
	Code Code
	Op   string // operation, e.g. "ListPaidBills"
	Err  error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Op != "" {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Op, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func wrapErr(code Code, op string, err error) error {
	return &Error{Code: code, Op: op, Err: err}
}

// IsCode reports whether err, or any error it wraps, carries code.
func IsCode(err error, code Code) bool {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Code == code
	}
	return false
}

var (
	// ErrNotAvailable is a sentinel for unmapped features.
	ErrNotAvailable = &Error{Code: ErrCodeNotAvailable, Err: errors.New("feature is not available in the resident portal for this unit")}
	// ErrConfirmationRequired is a sentinel for gated actions.
	ErrConfirmationRequired = &Error{Code: ErrCodeConfirmationRequired, Err: errors.New("action requires an explicit preview and confirm=true")}
)
