package condo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
)

// Action tiers, documented in README:
//   - Read: no confirmation needed.
//   - Consequential (request, reservation, and any future pay/vote/change):
//     preview first, then explicit Confirm=true matching that preview.
//   - Destructive: preview + Confirm=true + caller re-stating the target.
//
// The SDK never executes a consequential action on a default or zero
// value. A call without confirmation returns the preview and
// ErrConfirmationRequired, with no side effect.

// RequestPreview is the exact request that would be sent.
type RequestPreview struct {
	Action  string `json:"action"`
	Title   string `json:"title"`
	Subject string `json:"subject"`
	// PreviewID is a deterministic fingerprint of title+subject, so a
	// caller can prove the confirmation matches the preview it saw.
	PreviewID string `json:"preview_id"`
	Confirm   bool   `json:"confirm"`
}

// ReservationPreview is the exact reservation that would be requested.
type ReservationPreview struct {
	Action    string `json:"action"`
	Area      string `json:"area"`
	Date      string `json:"date"`
	Period    string `json:"period"`
	PreviewID string `json:"preview_id"`
	Confirm   bool   `json:"confirm"`
}

func previewID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// PreviewRequest validates and returns a preview. It sends nothing.
func PreviewRequest(title, subject string) (RequestPreview, error) {
	title, subject = strings.TrimSpace(title), strings.TrimSpace(subject)
	if title == "" {
		return RequestPreview{}, wrapErr(ErrCodeValidation, "PreviewRequest", fmt.Errorf("title is required"))
	}
	if subject == "" {
		return RequestPreview{}, wrapErr(ErrCodeValidation, "PreviewRequest", fmt.Errorf("subject is required"))
	}
	return RequestPreview{Action: "create_request", Title: title, Subject: subject, PreviewID: previewID("request", title, subject)}, nil
}

// CreateRequest executes a previewed request only when confirm is true
// and the preview is valid. The underlying submission endpoint for the
// /tickets modal is undiscovered (see README Limitations), so a confirmed
// call currently returns ErrCodeSubmissionUndiscovered and sends nothing,
// rather than guessing an endpoint or reporting false success.
func (c *Client) CreateRequest(_ context.Context, preview RequestPreview, confirm bool) (RequestPreview, error) {
	const op = "CreateRequest"
	if !confirm {
		return preview, wrapErr(ErrCodeConfirmationRequired, op, fmt.Errorf("call CreateRequest again with confirm=true and the same preview"))
	}
	if preview.PreviewID != previewID("request", preview.Title, preview.Subject) {
		return preview, wrapErr(ErrCodeValidation, op, fmt.Errorf("preview does not match its title and subject"))
	}
	if strings.TrimSpace(preview.Title) == "" || strings.TrimSpace(preview.Subject) == "" {
		return preview, wrapErr(ErrCodeValidation, op, fmt.Errorf("title and subject are required"))
	}
	out := preview
	out.Confirm = true
	return out, wrapErr(ErrCodeSubmissionUndiscovered, op, fmt.Errorf("request submission endpoint for /tickets is undiscovered; no request was sent"))
}

// PreviewReservation validates and returns a preview. It sends nothing.
func PreviewReservation(area, date, period string) (ReservationPreview, error) {
	area, date, period = strings.TrimSpace(area), strings.TrimSpace(date), strings.TrimSpace(period)
	if area == "" {
		return ReservationPreview{}, wrapErr(ErrCodeValidation, "PreviewReservation", fmt.Errorf("area is required"))
	}
	if date == "" {
		return ReservationPreview{}, wrapErr(ErrCodeValidation, "PreviewReservation", fmt.Errorf("date is required"))
	}
	if period == "" {
		return ReservationPreview{}, wrapErr(ErrCodeValidation, "PreviewReservation", fmt.Errorf("period is required"))
	}
	return ReservationPreview{Action: "pre_reserve_reservation", Area: area, Date: date, Period: period, PreviewID: previewID("reservation", area, date, period)}, nil
}

// PreReserveReservation executes a previewed reservation only when confirm
// is true. The underlying submission endpoint for the /reservas dialog is
// undiscovered, so a confirmed call returns ErrCodeSubmissionUndiscovered
// and sends nothing, rather than guessing an endpoint.
func (c *Client) PreReserveReservation(_ context.Context, preview ReservationPreview, confirm bool) (ReservationPreview, error) {
	const op = "PreReserveReservation"
	if !confirm {
		return preview, wrapErr(ErrCodeConfirmationRequired, op, fmt.Errorf("call PreReserveReservation again with confirm=true and the same preview"))
	}
	if preview.PreviewID != previewID("reservation", preview.Area, preview.Date, preview.Period) {
		return preview, wrapErr(ErrCodeValidation, op, fmt.Errorf("preview does not match its area, date, and period"))
	}
	out := preview
	out.Confirm = true
	return out, wrapErr(ErrCodeSubmissionUndiscovered, op, fmt.Errorf("reservation submission endpoint for /reservas is undiscovered; no reservation was made"))
}

// DestructiveAction is a helper for future destructive operations: it
// requires preview, confirm, and an exact re-statement of the target.
// It is exported so tool authors cannot accidentally add a destructive
// operation without this gate.
func ValidateDestructive(previewID, target, restatedTarget string, confirm bool) error {
	if !confirm {
		return wrapErr(ErrCodeConfirmationRequired, "DestructiveAction", fmt.Errorf("destructive actions require confirm=true"))
	}
	if strings.TrimSpace(target) == "" || target != restatedTarget {
		return wrapErr(ErrCodeValidation, "DestructiveAction", fmt.Errorf("destructive actions require the caller to re-state the exact target"))
	}
	if strings.TrimSpace(previewID) == "" {
		return wrapErr(ErrCodeValidation, "DestructiveAction", fmt.Errorf("preview id is required"))
	}
	return nil
}
