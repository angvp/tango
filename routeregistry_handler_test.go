package tango

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouteRegistryHandlerBeforeRunRegistrationFails(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}

	if _, err := routes.Handler(); err == nil {
		t.Fatal("Handler() returned nil error before RunRegistration, want non-nil")
	}
}

func TestRouteRegistryHandlerDispatchesMatchedRoute(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()

	view := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
	}
	if err := routes.Include("/users/", []Route{Path("GET", "/", view)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouteRegistryHandlerExposesPathParams(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()

	var gotID string
	view := func(ctx *Context) error {
		gotID = ctx.Param("id")
		return ctx.JSON(http.StatusOK, nil)
	}
	if err := routes.Include("/users/", []Route{Path("GET", "/{id}", view)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotID != "42" {
		t.Fatalf("ctx.Param(%q) = %q, want %q", "id", gotID, "42")
	}
}

func TestRouteRegistryHandlerUnmatchedPathReturns404(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRouteRegistryHandlerWrongMethodReturns405(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/users/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestRouteRegistryHandlerViewErrorReturnsGeneric500(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()

	view := func(ctx *Context) error {
		return errBoom
	}
	if err := routes.Include("/users/", []Route{Path("GET", "/", view)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/users/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	if _, hasError := body["error"]; !hasError {
		t.Fatalf("body = %v, want an %q key", body, "error")
	}
	for _, v := range body {
		if v == errBoom.Error() {
			t.Fatalf("response body leaked the underlying error message: %v", body)
		}
	}
}

func TestRouteRegistryHandlerCrossAppDuplicateNameFails(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()

	if err := routes.Include("/users/", []Route{Path("GET", "/", noopView, Name("index"))}); err != nil {
		t.Fatalf("first Include returned error: %v", err)
	}
	if err := routes.Include("/users/", []Route{Path("GET", "/all", noopView, Name("index"))}); err != nil {
		t.Fatalf("second Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	if _, err := routes.Handler(); err == nil {
		t.Fatal("Handler() returned nil error for cross-app duplicate name, want non-nil")
	}
}

func TestRouteRegistryHandlerReturnsPlainHTTPHandler(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{Path("GET", "/", noopView)}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/users/")
	if err != nil {
		t.Fatalf("http.Get returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
