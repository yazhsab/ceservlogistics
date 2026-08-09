// Package apierr defines the platform-wide API error model.
//
// Constitution §32: every error response uses one envelope:
//
//	{"error":{"code":"...","message":"...","details":{},"requestId":"..."}}
//
// Stack traces and driver messages never reach the client. Internal detail is
// carried on the Error value (via Unwrap) for logging only.
package apierr

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is a stable, machine-readable error identifier. The frontend switches on
// these; they are part of the published contract and must not be renamed
// without a documented migration.
type Code string

// Platform-level codes.
const (
	CodeBadRequest         Code = "BAD_REQUEST"
	CodeValidationFailed   Code = "VALIDATION_FAILED"
	CodeUnauthorized       Code = "UNAUTHORIZED"
	CodeInvalidCredentials Code = "INVALID_CREDENTIALS"
	CodeTokenExpired       Code = "TOKEN_EXPIRED"
	CodeTokenInvalid       Code = "TOKEN_INVALID"
	CodeTokenRevoked       Code = "TOKEN_REVOKED"
	CodeForbidden          Code = "FORBIDDEN"
	CodeAccountInactive    Code = "ACCOUNT_INACTIVE"
	CodeAccountLocked      Code = "ACCOUNT_LOCKED"
	CodeNotFound           Code = "NOT_FOUND"
	CodeMethodNotAllowed   Code = "METHOD_NOT_ALLOWED"
	CodeConflict           Code = "CONFLICT"
	CodeDuplicate          Code = "DUPLICATE_RESOURCE"
	CodeResourceInUse      Code = "RESOURCE_IN_USE"
	CodePayloadTooLarge    Code = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMedia   Code = "UNSUPPORTED_MEDIA_TYPE"
	CodeRateLimited        Code = "RATE_LIMITED"
	CodeInternal           Code = "INTERNAL_ERROR"
	CodeUnavailable        Code = "SERVICE_UNAVAILABLE"
	CodeTenantMismatch     Code = "TENANT_MISMATCH"
	CodeImmutableResource  Code = "IMMUTABLE_RESOURCE"

	CodeIdempotencyKeyRequired Code = "IDEMPOTENCY_KEY_REQUIRED"
	CodeIdempotencyKeyReuse    Code = "IDEMPOTENCY_KEY_REUSED"
	CodeIdempotencyInProgress  Code = "IDEMPOTENCY_IN_PROGRESS"

	CodeConcurrentModification Code = "CONCURRENT_MODIFICATION"
)

// Error is the canonical application error. Handlers return it; the HTTP layer
// renders it. Internal causes are attached with WithCause and are logged, never
// serialised.
type Error struct {
	Status  int
	Code    Code
	Message string
	Details map[string]any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an internal error for logging. The cause is never rendered
// to clients.
func (e *Error) WithCause(err error) *Error {
	c := *e
	c.cause = err
	return &c
}

// WithDetail adds one structured detail entry.
func (e *Error) WithDetail(key string, value any) *Error {
	c := *e
	c.Details = make(map[string]any, len(e.Details)+1)
	for k, v := range e.Details {
		c.Details[k] = v
	}
	c.Details[key] = value
	return &c
}

// WithDetails replaces the detail map.
func (e *Error) WithDetails(d map[string]any) *Error {
	c := *e
	c.Details = d
	return &c
}

// New builds an Error.
func New(status int, code Code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Newf builds an Error with a formatted message. Format arguments must never
// include secrets or raw driver output.
func Newf(status int, code Code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Constructors for the common cases.

func BadRequest(msg string) *Error { return New(http.StatusBadRequest, CodeBadRequest, msg) }

func Validation(msg string, fields map[string]any) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: CodeValidationFailed, Message: msg, Details: fields}
}

func Unauthorized(code Code, msg string) *Error {
	return New(http.StatusUnauthorized, code, msg)
}

func Forbidden(msg string) *Error { return New(http.StatusForbidden, CodeForbidden, msg) }

// NotFound deliberately uses a generic message. Distinguishing "does not exist"
// from "exists but belongs to another tenant" would leak cross-tenant
// information (Constitution §9), so both paths produce this identical response.
func NotFound(resource string) *Error {
	return Newf(http.StatusNotFound, CodeNotFound, "%s not found.", resource)
}

func Conflict(code Code, msg string) *Error { return New(http.StatusConflict, code, msg) }

func Duplicate(msg string) *Error { return New(http.StatusConflict, CodeDuplicate, msg) }

func Internal(err error) *Error {
	return (&Error{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternal,
		Message: "An unexpected error occurred. The incident has been recorded.",
	}).WithCause(err)
}

func Unavailable(msg string) *Error {
	return New(http.StatusServiceUnavailable, CodeUnavailable, msg)
}

func RateLimited(msg string) *Error {
	return New(http.StatusTooManyRequests, CodeRateLimited, msg)
}

// From converts any error into an *Error, defaulting to a redacted 500.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var ae *Error
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err)
}

// IsCode reports whether err carries the given code.
func IsCode(err error, code Code) bool {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code == code
	}
	return false
}

// Envelope is the wire representation.
type Envelope struct {
	Error Body `json:"error"`
}

// Body is the error payload.
type Body struct {
	Code      Code           `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"requestId,omitempty"`
}
