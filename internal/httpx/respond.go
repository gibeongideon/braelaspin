// Package httpx holds the router, the middleware stack, and the one JSON
// error envelope every endpoint uses.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Error is the single response shape for every failure.
//
//	{"error":{"code":"insufficient_funds","message":"...","meta":{...},"request_id":"..."}}
//
// `code` is stable and machine-readable; the Flutter client owns the
// user-facing copy, so the server stays terse and every string the player
// reads lives in one Dart file.
type Error struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Meta      map[string]any `json:"meta,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
}

type errorBody struct {
	Error Error `json:"error"`
}

// APIError is an error carrying the HTTP status and client-facing code.
type APIError struct {
	Status  int
	Code    string
	Message string
	Meta    map[string]any
	// Err is the underlying cause. It is logged, never sent to the client.
	Err error
}

func (e *APIError) Error() string {
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code + ": " + e.Message
}
func (e *APIError) Unwrap() error { return e.Err }

func Errorf(status int, code, message string) *APIError {
	return &APIError{Status: status, Code: code, Message: message}
}

func (e *APIError) WithMeta(k string, v any) *APIError {
	if e.Meta == nil {
		e.Meta = map[string]any{}
	}
	e.Meta[k] = v
	return e
}

func (e *APIError) WithCause(err error) *APIError {
	e.Err = err
	return e
}

// Common errors, defined once so codes cannot drift between handlers.
var (
	ErrBadRequest   = func(msg string) *APIError { return Errorf(http.StatusBadRequest, "bad_request", msg) }
	ErrUnauthorized = func() *APIError {
		return Errorf(http.StatusUnauthorized, "unauthorized", "Sign in to continue.")
	}
	ErrForbidden = func() *APIError {
		return Errorf(http.StatusForbidden, "forbidden", "You do not have access to this.")
	}
	ErrNotFound = func() *APIError {
		return Errorf(http.StatusNotFound, "not_found", "Not found.")
	}
	ErrConflict = func(code, msg string) *APIError { return Errorf(http.StatusConflict, code, msg) }
	ErrRateLimited = func() *APIError {
		return Errorf(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Please wait and try again.")
	}
	ErrInternal = func() *APIError {
		return Errorf(http.StatusInternalServerError, "internal", "Something went wrong on our side.")
	}
)

// JSON writes a successful response.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so there is nothing to salvage;
		// record it and move on.
		slog.Error("httpx: encode response", "err", err)
	}
}

// Raw writes a pre-encoded JSON body. Used by the idempotency replay path,
// which must return the original response byte-identically.
func Raw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// Fail writes an error response, logging the underlying cause but never
// leaking it to the client.
func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		// An unmapped error is a bug: the handler should have classified it.
		// Log the detail, tell the client nothing.
		apiErr = ErrInternal().WithCause(err)
	}

	reqID := RequestID(r.Context())
	lg := Logger(r.Context())

	if apiErr.Status >= 500 {
		lg.Error("request failed",
			"code", apiErr.Code, "status", apiErr.Status, "err", apiErr.Error())
	} else {
		// 4xx is the client's problem, not an incident. Debug level keeps
		// wrong-password noise out of the error stream.
		lg.Debug("request rejected",
			"code", apiErr.Code, "status", apiErr.Status, "err", apiErr.Error())
	}

	body := errorBody{Error: Error{
		Code:      apiErr.Code,
		Message:   apiErr.Message,
		Meta:      apiErr.Meta,
		RequestID: reqID,
	}}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.Status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("httpx: encode error response", "err", err)
	}
}

// Decode reads a JSON request body with a size limit and strict field checking.
//
// DisallowUnknownFields is deliberate: a client sending `{"amount": 100}` when
// the field is `amount_cents` should get a clear rejection rather than a
// silent zero, which on a money endpoint is the difference between an error
// and a wrong transaction.
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	const maxBody = 64 << 10
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return ErrBadRequest("The request body could not be read.").WithCause(err)
	}
	// Reject trailing content so `{...}{...}` is not silently accepted.
	if dec.More() {
		return ErrBadRequest("The request body must contain a single JSON object.")
	}
	return nil
}
