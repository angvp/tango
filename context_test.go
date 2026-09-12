package tango

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContextQueryReturnsValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/?name=alice", nil)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	if got := ctx.Query("name"); got != "alice" {
		t.Fatalf("Query(%q) = %q, want %q", "name", got, "alice")
	}
}

func TestContextQueryMissingReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	if got := ctx.Query("missing"); got != "" {
		t.Fatalf("Query(%q) = %q, want empty string", "missing", got)
	}
}

func TestContextParamReturnsValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	ctx := newContext(httptest.NewRecorder(), r, map[string]string{"id": "42"})

	if got := ctx.Param("id"); got != "42" {
		t.Fatalf("Param(%q) = %q, want %q", "id", got, "42")
	}
}

func TestContextParamMissingReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	ctx := newContext(httptest.NewRecorder(), r, map[string]string{"id": "42"})

	if got := ctx.Param("missing"); got != "" {
		t.Fatalf("Param(%q) = %q, want empty string", "missing", got)
	}
}

func TestContextBindDecodesJSONBody(t *testing.T) {
	body := strings.NewReader(`{"name":"alice"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	var payload struct {
		Name string `json:"name"`
	}
	if err := ctx.Bind(&payload); err != nil {
		t.Fatalf("Bind returned error: %v", err)
	}
	if payload.Name != "alice" {
		t.Fatalf("Bind decoded Name = %q, want %q", payload.Name, "alice")
	}
}

func TestContextBindMalformedBodyReturnsError(t *testing.T) {
	body := strings.NewReader(`{"name":`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	var payload struct {
		Name string `json:"name"`
	}
	if err := ctx.Bind(&payload); err == nil {
		t.Fatal("Bind returned nil error for malformed JSON, want non-nil")
	}
}

func TestContextJSONWritesStatusAndBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ctx := newContext(rec, r, nil)

	payload := map[string]string{"hello": "world"}
	if err := ctx.JSON(http.StatusCreated, payload); err != nil {
		t.Fatalf("JSON returned error: %v", err)
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", ct, "application/json")
	}

	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	if got["hello"] != "world" {
		t.Fatalf("body = %v, want %v", got, payload)
	}
}

func TestContextRedirectWritesRedirectResponse(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ctx := newContext(rec, r, nil)

	if err := ctx.Redirect("/elsewhere"); err != nil {
		t.Fatalf("Redirect returned error: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/elsewhere" {
		t.Fatalf("Location = %q, want %q", loc, "/elsewhere")
	}
}

func TestContextRequestReturnsWrappedRequest(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	if ctx.Request() != r {
		t.Fatal("Request() did not return the exact wrapped request")
	}
}

func TestContextResponseWriterReturnsWrappedWriter(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ctx := newContext(rec, r, nil)

	if ctx.ResponseWriter() != rec {
		t.Fatal("ResponseWriter() did not return the exact wrapped writer")
	}
}

func TestContextContextReturnsRequestContext(t *testing.T) {
	type key struct{}
	base := context.WithValue(context.Background(), key{}, "value")
	r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(base)
	ctx := newContext(httptest.NewRecorder(), r, nil)

	if ctx.Context() != r.Context() {
		t.Fatal("Context() did not return the wrapped request's context.Context")
	}
}
