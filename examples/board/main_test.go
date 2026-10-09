package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"

	"board/migrations"
	"board/project"
)

// testApp is the real application — same apps, routes, and middleware as
// main — on a fresh, fully migrated database.
type testApp struct {
	handler  http.Handler
	store    *db.Store
	registry *tango.Registry
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // keep test output quiet

	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(sqlDB, dialect)
	t.Setenv("BOARD_JWT_SECRET", strings.Repeat("s", jwt.MinimumSecretBytes))

	registry, err := tango.BuildRegistry(project.Config(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	registry.SetStore(store)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{handler: handler, store: store, registry: registry}
}

// do sends one request through the app and returns the recorded response.
func (a *testApp) do(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

// signUp creates an active account directly in the database.
func (a *testApp) signUp(t *testing.T, email, password string) {
	t.Helper()
	meta, _ := a.registry.Models().Get("Account")
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	account := accounts.Account{Email: email, PasswordHash: hash, Active: true, CreatedAt: time.Now().UTC()}
	if err := a.store.Create(t.Context(), meta, &account); err != nil {
		t.Fatal(err)
	}
}

// token logs in through the API and returns a bearer token.
func (a *testApp) token(t *testing.T, email, password string) string {
	t.Helper()
	rec := a.do("POST", "/api/token/", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("token: status %d: %s", rec.Code, rec.Body)
	}
	var body struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}

func TestAppPassesChecks(t *testing.T) {
	app := newTestApp(t)
	if err := tango.Check(project.Config(app.store)); err != nil {
		t.Fatal(err)
	}
}

func TestTokenEndpoint(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")

	tests := []struct {
		name, body string
		want       int
	}{
		{"right password", `{"email":"ana@example.com","password":"correct horse battery"}`, http.StatusOK},
		{"email is case-insensitive", `{"email":"ANA@example.com","password":"correct horse battery"}`, http.StatusOK},
		{"wrong password", `{"email":"ana@example.com","password":"nope"}`, http.StatusUnauthorized},
		{"unknown email", `{"email":"bo@example.com","password":"nope"}`, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := app.do("POST", "/api/token/", tt.body, ""); rec.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestCreatingAPostNeedsAToken(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	token := app.token(t, "ana@example.com", "correct horse battery")

	if rec := app.do("POST", "/posts/", `{"title":"x"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: status = %d, want 401", rec.Code)
	}
	if rec := app.do("POST", "/posts/", `{"title":"x"}`, "not-a-token"); rec.Code != http.StatusUnauthorized {
		t.Errorf("with a bad token: status = %d, want 401", rec.Code)
	}

	rec := app.do("POST", "/posts/", `{"title":"Hello","AccountID":999}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("with a token: status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var post struct{ AccountID int64 }
	json.Unmarshal(rec.Body.Bytes(), &post)
	if post.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1 (from the token, not the body)", post.AccountID)
	}
}

func TestOnlyTheOwnerCanDeleteAPost(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	app.signUp(t, "bo@example.com", "another good password")
	ana := app.token(t, "ana@example.com", "correct horse battery")
	bo := app.token(t, "bo@example.com", "another good password")

	app.do("POST", "/posts/", `{"title":"Ana's post"}`, ana)
	app.do("POST", "/posts/1/comments/", `{"author":"bo","body":"Nice"}`, "")

	if rec := app.do("DELETE", "/posts/1/", "", bo); rec.Code != http.StatusForbidden {
		t.Errorf("someone else's post: status = %d, want 403", rec.Code)
	}
	if rec := app.do("DELETE", "/posts/1/", "", ana); rec.Code != http.StatusNoContent {
		t.Errorf("own post: status = %d, want 204", rec.Code)
	}
	if rec := app.do("GET", "/posts/1/", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", rec.Code)
	}
	if rec := app.do("GET", "/posts/1/comments/", "", ""); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("comments after delete = %s, want [] (they cascade)", rec.Body)
	}
}

func TestFrontPageEscapesTitles(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	token := app.token(t, "ana@example.com", "correct horse battery")
	app.do("POST", "/posts/", `{"title":"<script>alert(1)</script>"}`, token)

	rec := app.do("GET", "/", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Error("the title was rendered as HTML")
	}
	if !strings.Contains(rec.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the escaped title is missing")
	}
}
