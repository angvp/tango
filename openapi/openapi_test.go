package openapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
)

func view(ctx *tango.Context) error { return ctx.JSON(http.StatusOK, map[string]string{}) }

// serve registers the shop app and the openapi app, runs registration, and
// returns the compiled handler.
func serve(t *testing.T, opts openapi.Options, routes ...func(*tango.Registry) error) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	for i, register := range routes {
		if err := registry.Register(tango.NewApp("app"+string(rune('a'+i)), register)); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Register(openapi.New(opts)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func fetch(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func shopRoutes(r *tango.Registry) error {
	return r.Routes().Include("/pets", []tango.Route{
		tango.Path("GET", "/", view, tango.Name("list"), tango.Doc(tango.RouteDoc{Summary: "List pets"})),
		tango.Path("GET", "/{id:[0-9]+}", view, tango.Name("detail")),
		tango.Path("DELETE", "/{id}", view),
		tango.Path("GET", "/files/*", view),
	})
}

func TestServesEveryRouteWithPathParameters(t *testing.T) {
	h := serve(t, openapi.Options{Title: "Pet shop", Version: "1.2.0"}, shopRoutes)
	rec := fetch(t, h, "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type %q", got)
	}
	idParam := []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}}
	want := map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": "Pet shop", "version": "1.2.0"},
		"tags":    []any{map[string]any{"name": "pets"}},
		"paths": map[string]any{
			"/pets/": map[string]any{"get": map[string]any{
				"operationId": "pets_list", "summary": "List pets", "tags": []any{"pets"},
				"responses": map[string]any{"default": map[string]any{"description": "Response"}},
			}},
			"/pets/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "pets_detail", "tags": []any{"pets"}, "parameters": idParam,
					"responses": map[string]any{"default": map[string]any{"description": "Response"}},
				},
				"delete": map[string]any{
					"operationId": "delete_pets_id", "tags": []any{"pets"}, "parameters": idParam,
					"responses": map[string]any{"default": map[string]any{"description": "Response"}},
				},
			},
			"/pets/files/{path}": map[string]any{"get": map[string]any{
				"operationId": "get_pets_files", "tags": []any{"pets"},
				"parameters": []any{map[string]any{"name": "path", "in": "path", "required": true, "description": "The rest of the path.", "schema": map[string]any{"type": "string"}}},
				"responses":  map[string]any{"default": map[string]any{"description": "Response"}},
			}},
		},
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("document differs\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestNothingIsServedUnlessInstalled(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Register(tango.NewApp("shop", shopRoutes)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	if rec := fetch(t, handler, "/openapi.json"); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestHiddenAndExcludedRoutesAreLeftOut(t *testing.T) {
	h := serve(t, openapi.Options{ExcludePrefixes: []string{"/admin/"}}, func(r *tango.Registry) error {
		return r.Routes().Include("", []tango.Route{
			tango.Path("GET", "/admin/", view),
			tango.Path("GET", "/secret", view, tango.Doc(tango.RouteDoc{Hidden: true})),
			tango.Path("GET", "/ping", view),
		})
	})
	var got struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(fetch(t, h, "/openapi.json").Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Paths) != 1 || got.Paths["/ping"] == nil {
		t.Fatalf("paths %v, want only /ping (the document's own route is never listed)", got.Paths)
	}
}

func TestOutputIsStableAndHonoursThePath(t *testing.T) {
	h := serve(t, openapi.Options{Path: "/api/spec.json"}, shopRoutes)
	a, b := fetch(t, h, "/api/spec.json").Body.String(), fetch(t, h, "/api/spec.json").Body.String()
	if a == "" || a != b {
		t.Fatal("document is empty or changes between requests")
	}
	if rec := fetch(t, h, "/openapi.json"); rec.Code != http.StatusNotFound {
		t.Fatalf("default path still served: %d", rec.Code)
	}
}

func TestRouteAndGlobalMiddlewareDoNotChangeTheListing(t *testing.T) {
	plain := serve(t, openapi.Options{}, shopRoutes)
	wrapped := serve(t, openapi.Options{}, func(r *tango.Registry) error {
		mw := func(next http.Handler) http.Handler { return next }
		return r.Routes().Include("/pets", []tango.Route{
			tango.Path("GET", "/", view, tango.Name("list"), tango.Use(mw), tango.Doc(tango.RouteDoc{Summary: "List pets"})),
			tango.Path("GET", "/{id:[0-9]+}", view, tango.Name("detail")),
			tango.Path("DELETE", "/{id}", view),
			tango.Path("GET", "/files/*", view),
		}, tango.WithMiddleware(mw))
	})
	if a, b := fetch(t, plain, "/openapi.json").Body.String(), fetch(t, wrapped, "/openapi.json").Body.String(); a != b {
		t.Fatalf("listing changed with middleware\n%s\n%s", a, b)
	}
}
