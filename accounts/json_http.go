package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/angvp/tango"
	"github.com/angvp/tango/internal/security"
)

// JSON mode's request mechanics, shared by every endpoint: the body rules,
// the response headers and the error shape.

const (
	// maxJSONBody is the most a JSON request body may hold.
	maxJSONBody = 16 * 1024
	// maxEmailLength is the longest email, in characters, JSON mode accepts:
	// RFC 5321's mailbox limit. It is request validation only; Account.Email
	// has no database length.
	maxEmailLength = 254
)

// apiError is an expected failure, answered with its status and the error
// shape {"error", "code", "fields"?}.
type apiError struct {
	status     int
	code       string
	message    string
	fields     map[string]string
	retryAfter string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func invalidBody(message string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: "invalid_body", message: message}
}

func fieldError(field, message string) *apiError {
	return &apiError{status: http.StatusUnprocessableEntity, code: "invalid_field", message: "invalid field", fields: map[string]string{field: message}}
}

func tooManyAttempts() *apiError {
	return &apiError{status: http.StatusTooManyRequests, code: "rate_limited", message: "too many attempts, try again later", retryAfter: "60"}
}

// view wraps an endpoint: every response, errors included, is JSON and
// no-store, because any of them can carry bearer credentials or account
// state. An error that is not an *apiError is logged like any View error and
// answered with a generic 500.
func (j *jsonAPI) view(endpoint func(ctx *tango.Context) error) tango.View {
	return func(ctx *tango.Context) error {
		header := ctx.ResponseWriter().Header()
		header.Set("Content-Type", "application/json")
		header.Set("Cache-Control", "no-store")
		err := endpoint(ctx)
		if err == nil {
			return nil
		}
		var apiErr *apiError
		if !errors.As(err, &apiErr) {
			ctx.Logger().LogAttrs(ctx.Context(), slog.LevelError, tango.EventViewError, slog.Any("error", err))
			apiErr = &apiError{status: http.StatusInternalServerError, code: "internal", message: "internal error"}
		}
		return writeAPIError(ctx, apiErr)
	}
}

func writeAPIError(ctx *tango.Context, e *apiError) error {
	if e.retryAfter != "" {
		ctx.ResponseWriter().Header().Set("Retry-After", e.retryAfter)
	}
	body := map[string]any{"error": e.message, "code": e.code}
	if len(e.fields) > 0 {
		body["fields"] = e.fields
	}
	return ctx.JSON(e.status, body)
}

// methodNotAllowedJSON answers a method an endpoint does not take.
func methodNotAllowedJSON(*tango.Context) error {
	return &apiError{status: http.StatusMethodNotAllowed, code: "method_not_allowed", message: "method not allowed"}
}

// readObject reads the request body as one JSON object and returns its
// members. It rejects, as invalid_body, anything but a single UTF-8 object
// with only the allowed keys, each once; a wrong content type is 415 and an
// oversized body 413.
func readObject(ctx *tango.Context, allowed ...string) (map[string]json.RawMessage, error) {
	mediaType, _, err := mime.ParseMediaType(ctx.Request().Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, &apiError{status: http.StatusUnsupportedMediaType, code: "unsupported_media_type", message: "send application/json"}
	}
	body, err := io.ReadAll(http.MaxBytesReader(ctx.ResponseWriter(), ctx.Request().Body, maxJSONBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, &apiError{status: http.StatusRequestEntityTooLarge, code: "body_too_large", message: "request body is too large"}
		}
		return nil, invalidBody("the request body could not be read")
	}
	if !utf8.Valid(body) {
		return nil, invalidBody("the request body is not valid UTF-8")
	}
	return parseObject(body, allowed)
}

func parseObject(body []byte, allowed []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, invalidBody("the request body must be one JSON object")
	}
	members := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, isString := keyToken.(string)
		if err != nil || !isString {
			return nil, invalidBody("the request body must be one JSON object")
		}
		if !contains(allowed, key) {
			return nil, invalidBody("unknown field " + key)
		}
		if _, duplicate := members[key]; duplicate {
			return nil, invalidBody("duplicate field " + key)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, invalidBody("the request body must be one JSON object")
		}
		members[key] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, invalidBody("the request body must be one JSON object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalidBody("unexpected data after the JSON object")
	}
	return members, nil
}

// stringMember returns members[key] as a string: "" when the key is absent,
// invalid_body when it is null or not a string.
func stringMember(members map[string]json.RawMessage, key string) (string, error) {
	raw, ok := members[key]
	if !ok {
		return "", nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return "", invalidBody(key + " must be a string")
	}
	return *value, nil
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}

// emailTooLong reports whether email is over maxEmailLength characters.
func emailTooLong(email string) bool { return utf8.RuneCountInString(email) > maxEmailLength }

// admit applies the failed-attempt limiter for the request's client.
func (j *jsonAPI) admit(ctx *tango.Context, limiter *security.RateLimiter) (key string, err error) {
	key = j.cfg.clientKey(ctx.Request())
	if !limiter.Allow(key) {
		return key, tooManyAttempts()
	}
	return key, nil
}
