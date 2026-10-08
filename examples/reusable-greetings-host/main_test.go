package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"
)

// TestTheInstalledReusableAppAnswersThroughTheHost is the example's point:
// the host installs a reusable app from another module, applies its
// contributed migrations with its own, and serves its routes and assets.
func TestTheInstalledReusableAppAnswersThroughTheHost(t *testing.T) {
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, allMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	registry, err := tango.BuildRegistry(appConfig(db.NewStore(sqlDB, dialect)))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}

	created := httptest.NewRecorder()
	handler.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/greetings/", strings.NewReader(`{"Name":"Ada"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("POST /greetings/ = %d %s", created.Code, created.Body.String())
	}
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/greetings/", nil))
	var greetings []struct{ Name string }
	if err := json.Unmarshal(listed.Body.Bytes(), &greetings); err != nil || len(greetings) != 1 || greetings[0].Name != "Ada" {
		t.Fatalf("GET /greetings/ = %d %s", listed.Code, listed.Body.String())
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/greetings/static/hello.txt", nil))
	if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
		t.Fatalf("GET /greetings/static/hello.txt = %d, want the reusable app's own asset", asset.Code)
	}
}
