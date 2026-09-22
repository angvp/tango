package main

import (
	"bytes"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	"notes-starter/migrations"

	_ "modernc.org/sqlite"
)

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := migration.ApplyPending(t.Context(), sqlDB, db.SQLite, migrations.Migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store := db.NewStore(sqlDB, db.SQLite)
	handler, err := buildHandler(appConfig(store), store)
	if err != nil {
		t.Fatalf("buildHandler: %v", err)
	}
	return handler
}

func postNote(handler http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	body := bytes.NewBufferString(`{"Title":"t","Body":"b","Private":false}`)
	req := httptest.NewRequest(http.MethodPost, "/api/notes/", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestCreateNoteIsRateLimitedPerIP(t *testing.T) {
	// A fresh package-level createLimiter is shared across this test file's
	// test functions (matches production: one limiter for the process'
	// lifetime), so use a distinct RemoteAddr per test to keep them
	// independent.
	const addr = "203.0.113.10:12345"
	handler := newTestHandler(t)

	for i := 0; i < 5; i++ {
		rec := postNote(handler, addr)
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: status = %d, want 201, body = %s", i, rec.Code, rec.Body.String())
		}
	}

	rec := postNote(handler, addr)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request: status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Fatal("expected a Retry-After header on the rate-limited response")
	}
}

func TestCreateNoteRateLimitIsPerIP(t *testing.T) {
	handler := newTestHandler(t)

	for i := 0; i < 5; i++ {
		rec := postNote(handler, "203.0.113.20:1111")
		if rec.Code != http.StatusCreated {
			t.Fatalf("first IP request %d: status = %d", i, rec.Code)
		}
	}
	// A different client IP starts with its own fresh bucket.
	rec := postNote(handler, "203.0.113.21:2222")
	if rec.Code != http.StatusCreated {
		t.Fatalf("second IP's first request: status = %d, want 201 (independent bucket)", rec.Code)
	}
}

func TestListNotesIsNotRateLimited(t *testing.T) {
	handler := newTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/notes/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
