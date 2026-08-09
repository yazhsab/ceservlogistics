// Package httpx contains the HTTP server, middleware chain and request/response
// helpers shared by every module's handlers.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ceserve/courier-os/internal/platform/apierr"
	"github.com/ceserve/courier-os/internal/platform/logging"
)

// Handler is a handler that may fail. Returning an error is the only way a
// handler reports failure; Wrap renders it through the single error envelope so
// no handler can accidentally invent its own error shape.
type Handler func(http.ResponseWriter, *http.Request) error

// Wrap adapts a Handler to http.Handler.
func Wrap(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// JSON writes a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if body == nil {
		return nil
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(body); err != nil {
		// The status line is already flushed; all we can do is record it.
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}

// OK writes 200 with a body.
func OK(w http.ResponseWriter, body any) error { return JSON(w, http.StatusOK, body) }

// Created writes 201 with a body and a Location header when provided.
func Created(w http.ResponseWriter, location string, body any) error {
	if location != "" {
		w.Header().Set("Location", location)
	}
	return JSON(w, http.StatusCreated, body)
}

// Accepted writes 202 for work handed to the background worker.
func Accepted(w http.ResponseWriter, body any) error { return JSON(w, http.StatusAccepted, body) }

// NoContent writes 204.
func NoContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// WriteError renders any error through the platform envelope and logs the
// internal cause. 5xx are logged at error level with the underlying cause;
// 4xx at debug level, because client mistakes are not operator incidents.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	ae := apierr.From(err)
	reqID := RequestID(r.Context())
	log := logging.FromContext(r.Context())

	attrs := []any{
		slog.String("error_code", string(ae.Code)),
		slog.Int("status", ae.Status),
		slog.String("path", r.URL.Path),
		slog.String("method", r.Method),
	}
	if cause := errors.Unwrap(ae); cause != nil {
		attrs = append(attrs, slog.String("cause", cause.Error()))
	}
	if ae.Status >= 500 {
		log.Error("request failed", attrs...)
	} else {
		log.Debug("request rejected", attrs...)
	}

	if ae.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="courier-os"`)
	}
	_ = JSON(w, ae.Status, apierr.Envelope{Error: apierr.Body{
		Code:      ae.Code,
		Message:   ae.Message,
		Details:   ae.Details,
		RequestID: reqID,
	}})
}

// DecodeJSON strictly decodes a JSON request body.
//
// Unknown fields are rejected. That is a deliberate mass-assignment control
// (Constitution §35): a client cannot smuggle organization_id, status or any
// other server-owned field into a request struct and have it silently ignored —
// it gets a 400 naming the field.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	ct := r.Header.Get("Content-Type")
	if ct != "" {
		mt := strings.TrimSpace(strings.Split(ct, ";")[0])
		if !strings.EqualFold(mt, "application/json") {
			return apierr.New(http.StatusUnsupportedMediaType, apierr.CodeUnsupportedMedia,
				"Content-Type must be application/json.")
		}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return apierr.New(http.StatusRequestEntityTooLarge, apierr.CodePayloadTooLarge,
				"Request body is larger than the permitted limit.")
		case errors.Is(err, io.EOF):
			return apierr.BadRequest("Request body must not be empty.")
		}
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			return apierr.Newf(http.StatusBadRequest, apierr.CodeBadRequest,
				"Request body contains malformed JSON at position %d.", syn.Offset)
		}
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return apierr.Validation("Request body contains a field of the wrong type.", map[string]any{
				"field":    typeErr.Field,
				"expected": typeErr.Type.String(),
			})
		}
		if f, ok := unknownField(err.Error()); ok {
			return apierr.Validation("Request body contains an unrecognised field.", map[string]any{
				"field": f,
			})
		}
		return apierr.BadRequest("Request body could not be parsed.")
	}
	// Reject trailing content so `{"a":1}{"b":2}` cannot be interpreted loosely.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apierr.BadRequest("Request body must contain exactly one JSON object.")
	}
	return nil
}

func unknownField(msg string) (string, bool) {
	const marker = "unknown field "
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	return strings.Trim(msg[i+len(marker):], `"`), true
}
