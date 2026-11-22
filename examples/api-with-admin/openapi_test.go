package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTheAPIDocumentIsServedOnlyInDevelopment pins the example's OpenAPI
// wiring: without TANGO_OPENAPI both paths answer 404, and with it the
// document describes the posts routes and Swagger UI loads.
func TestTheAPIDocumentIsServedOnlyInDevelopment(t *testing.T) {
	t.Setenv("TANGO_OPENAPI", "")
	off, _, _ := newTestServer(t)
	for _, path := range []string{"/openapi.json", "/docs/"} {
		rec := httptest.NewRecorder()
		off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s without TANGO_OPENAPI = %d, want 404", path, rec.Code)
		}
	}

	t.Setenv("TANGO_OPENAPI", "1")
	on, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d, want 200: %s", rec.Code, rec.Body)
	}
	var doc struct {
		Info  struct{ Title string } `json:"info"`
		Paths map[string]map[string]struct {
			Summary    string `json:"summary"`
			Parameters []struct {
				Name string `json:"name"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Title != "Posts API" {
		t.Errorf("title = %q", doc.Info.Title)
	}
	list := doc.Paths["/posts/"]["get"]
	if list.Summary != "List posts, newest first" {
		t.Errorf("list summary = %q", list.Summary)
	}
	var names []string
	for _, p := range list.Parameters {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "author_id,q" {
		t.Errorf("list query parameters = %q, want author_id,q", got)
	}
	if doc.Paths["/posts/"]["post"].Summary == "" || doc.Paths["/posts/{id}/"]["get"].Summary == "" {
		t.Errorf("create and detail routes are not described: %v", doc.Paths)
	}
	for path := range doc.Paths {
		if strings.HasPrefix(path, "/admin") {
			t.Errorf("the admin's page %s is listed", path)
		}
	}

	ui := httptest.NewRecorder()
	on.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if ui.Code != http.StatusOK {
		t.Fatalf("GET /docs/ = %d, want 200", ui.Code)
	}
	if !strings.Contains(ui.Header().Get("Content-Security-Policy"), "style-src") {
		t.Errorf("the Swagger UI page carries no policy: %q", ui.Header().Get("Content-Security-Policy"))
	}
}
