package admin_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
	_ "modernc.org/sqlite"
)

// --- Malformed primary key in the URL path (edit/delete) ---

// TestEditAndDeleteViewsRejectMalformedPrimaryKeyInURL covers
// parsePKValue's error path as a framework user would actually hit it: a
// non-numeric path segment where an int64 primary key is expected (e.g. a
// stale link, a typo, or a bot probing URLs) must 404, not panic or 500.
func TestEditAndDeleteViewsRejectMalformedPrimaryKeyInURL(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	for _, path := range []string{
		crudBasePath + "not-a-number/",
		crudBasePath + "not-a-number/delete/",
	} {
		t.Run(path, func(t *testing.T) {
			response := doRequest(t, handler, http.MethodGet, path, nil)
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
			}
		})
	}
}

// --- indexView (admin root) ---

// TestIndexViewRendersEmptyStateWhenNoModelIsRegistered covers indexView's
// other branch (no registered model to redirect to): a project that has
// installed the admin app but not yet registered any model with it still
// gets a coherent page at /admin/, not a redirect to nowhere or an error.
func TestIndexViewRendersEmptyStateWhenNoModelIsRegistered(t *testing.T) {
	// buildLoginTestHandler wires up admin.New with an admin account and no
	// other registered model, which is exactly the empty-nav state this
	// test needs.
	handler, _ := buildLoginTestHandler(t)

	cookie := loginAndGetSessionCookie(t, handler, "admin", "correct-password")
	request := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "No models registered yet") {
		t.Fatalf("body does not contain the empty-state message:\n%s", response.Body.String())
	}
}

// --- login POST with an unparsable request body ---

// TestLoginPostWithUnreadableBodyReturnsError covers loginView's
// ParseForm error branch: a POST whose body can't be read (here, a reader
// that always errors, simulating a client that drops the connection
// mid-upload) surfaces as an error rather than silently proceeding with
// empty credentials.
func TestLoginPostWithUnreadableBodyReturnsError(t *testing.T) {
	handler, _ := buildLoginTestHandler(t)
	csrfCookie := fetchLoginCSRF(t, handler)

	request := httptest.NewRequest(http.MethodPost, "/admin/login/", &alwaysErrorReader{})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.ContentLength = -1
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code < 500 {
		t.Fatalf("status = %d, want a server error for an unreadable request body", response.Code)
	}
}

type alwaysErrorReader struct{}

func (*alwaysErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

// --- create POST with an unreadable body / a store error ---

// TestCreateViewPostWithUnreadableBodyReturnsError covers createView's
// ParseForm error branch, mirroring loginView's.
func TestCreateViewPostWithUnreadableBodyReturnsError(t *testing.T) {
	handler, _ := buildProductAdmin(t)

	request := httptest.NewRequest(http.MethodPost, crudBasePath+"new/", &alwaysErrorReader{})
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.ContentLength = -1
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code < 500 {
		t.Fatalf("status = %d, want a server error for an unreadable request body", response.Code)
	}
}

// TestCreateViewPropagatesStoreErrorOnDuplicateUniqueField covers
// createView's store.Create error branch with a real, reachable cause: a
// unique-constrained field (adminShellUser.Email) colliding with an
// existing row. createView has no special handling for this (unlike
// accounts.registerView's IsUniqueConstraintViolation branch) — the
// database error propagates as-is, which this test locks in.
func TestCreateViewPropagatesStoreErrorOnDuplicateUniqueField(t *testing.T) {
	handler, _, sqlDB := buildAdminHandlerWithStore(t)
	// buildAdminHandlerWithStore's raw CREATE TABLE has no UNIQUE constraint
	// of its own (the model's tango:"unique" tag only matters to tanGO's
	// own migration generator, not this hand-written test schema), so add
	// one directly to make a genuine duplicate-email insert fail at the
	// database level, the way it would in a migrated project.
	if _, err := sqlDB.Exec(`CREATE UNIQUE INDEX admin_shell_user_email_unique ON admin_shell_user (email)`); err != nil {
		t.Fatalf("add unique index: %v", err)
	}

	cookie := loginAndGetSessionCookie(t, handler, "admin", "secret")
	response := doAdminShellUserCreate(t, handler, cookie, "seed@example.com")

	if response.Code < 500 {
		t.Fatalf("status = %d, want a server error for a duplicate unique field", response.Code)
	}
}

func doAdminShellUserCreate(t *testing.T, handler http.Handler, cookie *http.Cookie, email string) *httptest.ResponseRecorder {
	t.Helper()
	sum := sha256.Sum256([]byte(cookie.Value))
	csrfToken := hex.EncodeToString(sum[:])
	form := url.Values{"Email": {email}, "csrf_token": {csrfToken}}
	request := httptest.NewRequest(http.MethodPost, "/admin/admin_shell_user/new/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// --- CreateAccount over bcrypt's password length limit ---

// TestCreateAccountRejectsPasswordOverBcryptLimit covers CreateAccount's
// bcrypt-error path: unlike accounts.registerView (public self-service
// registration), admin's CreateAccount is operator-run via the CLI and
// deliberately enforces no password length minimum or maximum of its own —
// see accounts/register.go's minPasswordLength/maxPasswordLength comment —
// so a password over bcrypt's own 72-byte limit must still surface as a
// clear error rather than a panic or a silently truncated hash.
func TestCreateAccountRejectsPasswordOverBcryptLimit(t *testing.T) {
	store := setupAccountStore(t)
	longPassword := strings.Repeat("a", 73)

	if err := admin.CreateAccount(context.Background(), store, "alice", longPassword); err == nil {
		t.Fatal("CreateAccount with a 73-byte password: want error, got nil")
	}
}
