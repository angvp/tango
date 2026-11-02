package tango

import (
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
)

var (
	errSentinelView    = errors.New("boom")
	errReadFailed      = errors.New("read failed")
	errCustomUnmarshal = errors.New("custom")
)

// failingBody is a request body whose read fails with a non-EOF error.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errReadFailed }

// failingUnmarshaler is a payload whose UnmarshalJSON returns err. The decoder
// hands that error back unchanged.
type failingUnmarshaler struct{ err error }

func (f *failingUnmarshaler) UnmarshalJSON([]byte) error { return f.err }

func bindFailingUnmarshaler(err error) View {
	return func(ctx *Context) error { return ctx.Bind(&failingUnmarshaler{err: err}) }
}

func bindName(ctx *Context) error {
	var payload struct {
		Name string `json:"name"`
	}
	if err := ctx.Bind(&payload); err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, payload)
}

// A View that returns Bind's error unhandled must answer a malformed body with
// 400, while the body-limit and generic-500 contracts stay unchanged.
func TestBindMalformedJSONResponses(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		global     []Middleware
		hideLength bool
		view       View
		wantStatus int
		wantError  string
	}{
		{name: "truncated object", body: `{"name":`, view: bindName, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
		{name: "syntax error", body: `{"name":}`, view: bindName, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
		{name: "type mismatch", body: `{"name":5}`, view: bindName, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
		{name: "empty body", body: ``, view: bindName, wantStatus: http.StatusBadRequest, wantError: "invalid request body"},
		{
			name:       "oversized body stays 413",
			body:       `{"name":"too long"}`,
			global:     []Middleware{MaxBodySize(8)},
			hideLength: true,
			view:       bindName,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  "request body too large",
		},
		{
			name:       "non-Bind view error stays 500",
			body:       `{"name":"ok"}`,
			view:       func(*Context) error { return errSentinelView },
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal error",
		},
		{
			name:       "custom UnmarshalJSON syntax error is malformed",
			body:       `{"name":"ok"}`,
			view:       bindFailingUnmarshaler(&json.SyntaxError{}),
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "custom UnmarshalJSON type error is malformed",
			body:       `{"name":"ok"}`,
			view:       bindFailingUnmarshaler(&json.UnmarshalTypeError{}),
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name:       "custom UnmarshalJSON error stays 500",
			body:       `{"name":"ok"}`,
			view:       bindFailingUnmarshaler(errCustomUnmarshal),
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal error",
		},
		{
			name:       "nil destination stays 500",
			body:       `{"name":"ok"}`,
			view:       func(ctx *Context) error { return ctx.Bind(nil) },
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal error",
		},
		{
			name:       "non-pointer destination stays 500",
			body:       `{"name":"ok"}`,
			view:       func(ctx *Context) error { var p struct{ Name string }; return ctx.Bind(p) },
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := buildBodyHandler(t, tt.view, tt.global)
			var body io.Reader = strings.NewReader(tt.body)
			if tt.hideLength {
				body = undeclaredBody{body}
			}
			response := post(handler, body, "application/json")
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %q; want %d", response.Code, response.Body.String(), tt.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q, want application/json", got)
			}
			var payload map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil ||
				!maps.Equal(payload, map[string]string{"error": tt.wantError}) {
				t.Fatalf("body = %q, want exactly {\"error\":%q}", response.Body.String(), tt.wantError)
			}
		})
	}
}

// Bind's error keeps the sentinel and the decoder's own error reachable, so a
// View can branch on either.
func TestBindMalformedJSONErrorChain(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		isTarget error
		asTarget any
	}{
		{name: "truncated object", body: `{"name":`, isTarget: io.ErrUnexpectedEOF},
		{name: "syntax error", body: `{"name":}`, asTarget: new(*json.SyntaxError)},
		{name: "type mismatch", body: `{"name":5}`, asTarget: new(*json.UnmarshalTypeError)},
		{name: "empty body", body: ``, isTarget: io.EOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			handler := buildBodyHandler(t, func(ctx *Context) error {
				var payload struct {
					Name string `json:"name"`
				}
				got = ctx.Bind(&payload)
				return got
			}, nil)
			post(handler, strings.NewReader(tt.body), "application/json")
			if got == nil {
				t.Fatal("Bind returned nil error for a bad body")
			}
			if !errors.Is(got, ErrInvalidBody) {
				t.Fatalf("errors.Is(err, ErrInvalidBody) = false for %v", got)
			}
			if tt.isTarget != nil && !errors.Is(got, tt.isTarget) {
				t.Fatalf("errors.Is(err, %v) = false for %v", tt.isTarget, got)
			}
			if tt.asTarget != nil && !errors.As(got, tt.asTarget) {
				t.Fatalf("errors.As(err, %T) = false for %v", tt.asTarget, got)
			}
		})
	}
}

// A JSON syntax error returned by a custom UnmarshalJSON is a malformed body,
// and the decoder's error stays reachable.
func TestBindCustomSyntaxErrorChain(t *testing.T) {
	syntaxErr := &json.SyntaxError{}
	var got error
	handler := buildBodyHandler(t, func(ctx *Context) error {
		got = bindFailingUnmarshaler(syntaxErr)(ctx)
		return got
	}, nil)
	post(handler, strings.NewReader(`{"name":"ok"}`), "application/json")
	if !errors.Is(got, ErrInvalidBody) {
		t.Fatalf("errors.Is(err, ErrInvalidBody) = false for %v", got)
	}
	var asErr *json.SyntaxError
	if !errors.As(got, &asErr) || asErr != syntaxErr {
		t.Fatalf("errors.As(err, *json.SyntaxError) did not return the custom error: %v", got)
	}
}

// Errors that are not a malformed body keep their own identity and do not
// match ErrInvalidBody.
func TestBindOtherErrorsAreNotInvalidBody(t *testing.T) {
	tests := []struct {
		name     string
		body     io.Reader
		global   []Middleware
		bind     View
		isTarget error
		asTarget any
	}{
		{
			name:     "custom UnmarshalJSON error",
			body:     strings.NewReader(`{"name":"ok"}`),
			bind:     bindFailingUnmarshaler(errCustomUnmarshal),
			isTarget: errCustomUnmarshal,
		},
		{
			name:     "nil destination",
			body:     strings.NewReader(`{"name":"ok"}`),
			bind:     func(ctx *Context) error { return ctx.Bind(nil) },
			asTarget: new(*json.InvalidUnmarshalError),
		},
		{
			name:     "non-pointer destination",
			body:     strings.NewReader(`{"name":"ok"}`),
			bind:     func(ctx *Context) error { var p struct{ Name string }; return ctx.Bind(p) },
			asTarget: new(*json.InvalidUnmarshalError),
		},
		{
			name:     "read failure",
			body:     failingBody{},
			bind:     bindName,
			isTarget: errReadFailed,
		},
		{
			name:     "oversized body",
			body:     undeclaredBody{strings.NewReader(`{"name":"too long"}`)},
			global:   []Middleware{MaxBodySize(8)},
			bind:     bindName,
			asTarget: new(*http.MaxBytesError),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got error
			handler := buildBodyHandler(t, func(ctx *Context) error {
				got = tt.bind(ctx)
				return got
			}, tt.global)
			post(handler, tt.body, "application/json")
			if got == nil {
				t.Fatal("Bind returned nil error")
			}
			if errors.Is(got, ErrInvalidBody) {
				t.Fatalf("errors.Is(err, ErrInvalidBody) = true for %v; want false", got)
			}
			if tt.isTarget != nil && !errors.Is(got, tt.isTarget) {
				t.Fatalf("errors.Is(err, %v) = false for %v", tt.isTarget, got)
			}
			if tt.asTarget != nil && !errors.As(got, tt.asTarget) {
				t.Fatalf("errors.As(err, %T) = false for %v", tt.asTarget, got)
			}
		})
	}
}
