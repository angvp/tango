package tango

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
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

func TestRouteRegistryHandlerRunsMiddlewareInGlobalGroupRouteOrder(t *testing.T) {
	registry := NewRegistry()
	registry.Routes().setMiddleware([]Middleware{recordMiddleware("global")})
	routes := registry.Routes()

	view := func(ctx *Context) error {
		order := append(ctx.Request().Context().Value(orderKey{}).([]string), "view")
		return ctx.JSON(http.StatusOK, map[string][]string{"order": order})
	}
	if err := routes.Include("/users/", []Route{
		Path("GET", "/", view, Use(recordMiddleware("route"))),
	}, WithMiddleware(recordMiddleware("group"))); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}

	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	var body map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	want := []string{"global", "group", "route", "view"}
	if !reflect.DeepEqual(body["order"], want) {
		t.Fatalf("order = %v, want %v", body["order"], want)
	}
}

func TestRouteRegistryMiddlewareScopesDoNotLeak(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	view := func(ctx *Context) error {
		order, _ := ctx.Request().Context().Value(orderKey{}).([]string)
		return ctx.JSON(http.StatusOK, map[string][]string{"order": append(order, "view")})
	}

	if err := routes.Include("/users/", []Route{
		Path("GET", "/", view, Use(recordMiddleware("route"))),
	}, WithMiddleware(recordMiddleware("group"))); err != nil {
		t.Fatalf("first Include returned error: %v", err)
	}
	if err := routes.Include("/posts/", []Route{Path("GET", "/", view)}); err != nil {
		t.Fatalf("second Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts/", nil))

	var body map[string][]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	want := []string{"view"}
	if !reflect.DeepEqual(body["order"], want) {
		t.Fatalf("order = %v, want %v", body["order"], want)
	}
}

func TestRouteRegistryMiddlewareCanPassValuesThroughRequestContext(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	type key struct{}

	setValue := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, "from-middleware")))
		})
	}
	view := func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{
			"value": ctx.Request().Context().Value(key{}).(string),
		})
	}
	if err := routes.Include("/users/", []Route{Path("GET", "/", view)}, WithMiddleware(setValue)); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	if body["value"] != "from-middleware" {
		t.Fatalf("value = %q, want %q", body["value"], "from-middleware")
	}
}

func TestRouteRegistryAcceptsChiShapedMiddlewareDirectly(t *testing.T) {
	var _ Middleware = middleware.NoCache

	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{
		Path("GET", "/", func(ctx *Context) error {
			return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
		}, Use(middleware.NoCache)),
	}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	if rec.Header().Get("Cache-Control") == "" {
		t.Fatal("Cache-Control header is empty, want chi middleware to run")
	}
}

func TestRecovererReturnsGeneric500AndLogsPanic(t *testing.T) {
	var logBuffer bytes.Buffer
	restoreLog := captureLogOutput(&logBuffer)
	defer restoreLog()

	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{
		Path("GET", "/", func(ctx *Context) error {
			panic("boom")
		}, Use(Recoverer())),
	}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body did not decode as JSON: %v", err)
	}
	if body["error"] != "internal error" {
		t.Fatalf("error body = %q, want %q", body["error"], "internal error")
	}
	if !bytes.Contains(logBuffer.Bytes(), []byte(EventPanic)) || !bytes.Contains(logBuffer.Bytes(), []byte("recovered=boom")) {
		t.Fatalf("log = %q, want panic logged", logBuffer.String())
	}
}

func TestRouteWithoutRecovererLeavesPanicUnrecovered(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{
		Path("GET", "/", func(ctx *Context) error {
			panic("boom")
		}),
	}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("ServeHTTP did not panic, want panic without Recoverer")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/", nil))
}

func TestRecovererHandlesPanicFromDownstreamMiddleware(t *testing.T) {
	registry := NewRegistry()
	routes := registry.Routes()
	panicMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("middleware boom")
		})
	}
	if err := routes.Include("/users/", []Route{
		Path("GET", "/", noopView, Use(Recoverer(), panicMiddleware)),
	}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestRecovererMatchesViewErrorResponseShape(t *testing.T) {
	panicResponse := responseForRoute(t, Path("GET", "/", func(ctx *Context) error {
		panic("boom")
	}, Use(Recoverer())))
	errorResponse := responseForRoute(t, Path("GET", "/", func(ctx *Context) error {
		return errors.New("boom")
	}))

	if panicResponse.Code != errorResponse.Code {
		t.Fatalf("panic status = %d, view error status = %d", panicResponse.Code, errorResponse.Code)
	}
	if panicResponse.Body.String() != errorResponse.Body.String() {
		t.Fatalf("panic body = %q, view error body = %q", panicResponse.Body.String(), errorResponse.Body.String())
	}
}

type orderKey struct{}

func recordMiddleware(marker string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			current, _ := r.Context().Value(orderKey{}).([]string)
			current = append(append([]string(nil), current...), marker)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), orderKey{}, current)))
		})
	}
}

func captureLogOutput(w io.Writer) func() {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(w)
	log.SetFlags(0)
	return func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}
}

func responseForRoute(t *testing.T, route Route) *httptest.ResponseRecorder {
	t.Helper()
	registry := NewRegistry()
	routes := registry.Routes()
	if err := routes.Include("/users/", []Route{route}); err != nil {
		t.Fatalf("Include returned error: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("RunRegistration returned error: %v", err)
	}
	handler, err := routes.Handler()
	if err != nil {
		t.Fatalf("Handler() returned error: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/users/", nil))
	return rec
}
