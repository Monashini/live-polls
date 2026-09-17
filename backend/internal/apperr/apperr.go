// Package apperr defines the single error type that crosses layer boundaries.
//
// The rule it enforces: services return *apperr.Error, handlers translate that
// into an HTTP status and a JSON body. A MongoDB driver error, a bcrypt error
// or a nil-pointer panic must never reach the client verbatim, because driver
// messages leak schema names, index names and sometimes connection strings.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Code is a stable, machine-readable label. The frontend switches on this,
// never on Message, so wording can change without breaking clients.
type Code string

const (
	CodeValidation      Code = "VALIDATION_FAILED"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeNotFound        Code = "NOT_FOUND"
	CodeConflict        Code = "CONFLICT"
	CodeAlreadyVoted    Code = "ALREADY_VOTED"
	CodePollClosed      Code = "POLL_CLOSED"
	CodePayloadTooLarge Code = "PAYLOAD_TOO_LARGE"
	CodeInternal        Code = "INTERNAL_ERROR"

	// CodeClientClosed covers "the caller hung up before we answered". It is
	// not a failure of ours, and must not be logged as one.
	CodeClientClosed Code = "CLIENT_CLOSED_REQUEST"

	CodeRateLimited Code = "RATE_LIMITED"
)

// StatusClientClosed is nginx's non-standard 499. Go has no constant for it.
// It never reaches a browser -- by definition the connection is already gone --
// but it keeps the access log honest about what happened.
const StatusClientClosed = 499

// Error carries everything the handler layer needs to build a response, plus
// a private cause that is logged server-side and never serialized.
type Error struct {
	Status  int
	Code    Code
	Message string
	// Fields maps a request field name to a human-readable problem, e.g.
	// {"options": "must contain between 2 and 10 choices"}.
	Fields map[string]string

	// RetryAfter is set on rate-limit errors and becomes the Retry-After
	// header. Zero means "no advice to give".
	RetryAfter time.Duration

	cause error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap lets errors.Is / errors.As reach the underlying cause in logs.
func (e *Error) Unwrap() error { return e.cause }

// Cause exposes the wrapped error for logging only.
func (e *Error) Cause() error { return e.cause }

func Validation(message string, fields map[string]string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeValidation, Message: message, Fields: fields}
}

func Unauthorized(message string) *Error {
	return &Error{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: message}
}

func Forbidden(message string) *Error {
	return &Error{Status: http.StatusForbidden, Code: CodeForbidden, Message: message}
}

func NotFound(message string) *Error {
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: message}
}

func Conflict(code Code, message string) *Error {
	return &Error{Status: http.StatusConflict, Code: code, Message: message}
}

// TooManyRequests carries a RetryAfter so the handler can set the header and
// the client knows how long to back off, rather than retrying immediately and
// making the problem worse.
func TooManyRequests(message string, retryAfter time.Duration) *Error {
	return &Error{
		Status:     http.StatusTooManyRequests,
		Code:       CodeRateLimited,
		Message:    message,
		RetryAfter: retryAfter,
	}
}

func TooLarge(message string) *Error {
	return &Error{Status: http.StatusRequestEntityTooLarge, Code: CodePayloadTooLarge, Message: message}
}

// Internal wraps an unexpected failure. The caller's error is preserved for
// the log; the client only ever sees the generic Message.
func Internal(cause error) *Error {
	return &Error{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternal,
		Message: "Something went wrong on our side.",
		cause:   cause,
	}
}

// From normalises any error into an *Error. Anything not already typed is
// treated as an internal failure, which is the safe default: an error we
// forgot to classify must not leak its text to the client.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}

// Envelope is the exact JSON shape every error response uses. It lives here,
// not in the handlers package, so the middleware and the handlers cannot drift
// into producing two different error formats. It deliberately has no knowledge
// of Gin or net/http.
type Envelope struct {
	Error Body `json:"error"`
}

type Body struct {
	Code      Code              `json:"code"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"requestId,omitempty"`
}

// Envelope builds the client-facing payload. Note it never includes cause:
// that is for the server log only.
func (e *Error) Envelope(requestID string) Envelope {
	return Envelope{Body{
		Code:      e.Code,
		Message:   e.Message,
		Fields:    e.Fields,
		RequestID: requestID,
	}}
}
