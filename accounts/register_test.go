package accounts_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/testdb"
)

func buildRegisterTestHandler(t *testing.T, opts ...accounts.Option) (http.Handler, *db.Store) {
	t.Helper()

	_, store := migratedAccountsDB(t)
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(store, opts...)); err != nil {
		t.Fatalf("register accounts app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, store
}

// fetchRegisterCSRF performs a GET /accounts/register/ and returns the
// resulting pre-session CSRF cookie every registration test needs before
// it can POST.
func fetchRegisterCSRF(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/accounts/register/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_pre_session_csrf" {
			return cookie
		}
	}
	t.Fatal("GET /accounts/register/ did not set a pre-session CSRF cookie")
	return nil
}

func postRegister(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	csrfCookie := fetchRegisterCSRF(t, handler)

	submitted := url.Values{}
	for key, values := range form {
		submitted[key] = values
	}
	submitted.Set("csrf_token", csrfCookie.Value)

	request := httptest.NewRequest(http.MethodPost, "/accounts/register/", strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestRegisterWithValidDataCreatesAccountWithHashedPassword(t *testing.T) {
	handler, store := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"Alice@Example.com"}, "password": {"correct-password"}})

	if response.Code == http.StatusBadRequest || response.Code == http.StatusConflict {
		t.Fatalf("status = %d, want a redirect (success), body: %s", response.Code, response.Body.String())
	}

	var row struct {
		Email        string
		PasswordHash string
		Active       bool
	}
	if err := store.QueryRow(context.Background(), &row, "SELECT email AS Email, password_hash AS PasswordHash, active AS Active FROM account WHERE email = $1", "alice@example.com"); err != nil {
		t.Fatalf("query account row: %v", err)
	}
	if row.PasswordHash == "correct-password" {
		t.Fatal("password stored as plaintext")
	}
	if !row.Active {
		t.Fatal("newly registered account is not Active")
	}
}

func TestRegisterRejectsPasswordUnderMinimumLength(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"bob@example.com"}, "password": {"short"}})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "at least 8 characters") {
		t.Fatalf("body = %q, want a password-length error", response.Body.String())
	}
}

func TestRegisterRejectsEmptyEmailOrPassword(t *testing.T) {
	tests := []struct {
		name  string
		email string
		pass  string
	}{
		{name: "empty email", email: "", pass: "correct-password"},
		{name: "empty password", email: "empty-pass@example.com", pass: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := buildRegisterTestHandler(t)
			response := postRegister(t, handler, url.Values{"email": {tt.email}, "password": {tt.pass}})

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if !strings.Contains(response.Body.String(), "required") {
				t.Fatalf("body = %q, want a required-fields error", response.Body.String())
			}
		})
	}
}

// TestRegisterRejectsPasswordOverBcryptLimit covers the fix for a bug this
// ticket surfaced: bcrypt hard-rejects passwords over 72 bytes
// (bcrypt.ErrPasswordTooLong), so a password-manager-generated password
// past that length used to reach bcrypt unchecked and surface as a raw
// 500. registerView now validates the length upfront and returns the same
// kind of friendly 400 as the existing too-short case.
func TestRegisterRejectsPasswordOverBcryptLimit(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{
		"email":    {"toolong@example.com"},
		"password": {strings.Repeat("a", 100)},
	})

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "72 characters") {
		t.Fatalf("body = %q, want a max-length validation message", response.Body.String())
	}
}

func TestRegisterDuplicateEmailIsCaseInsensitiveAndSaysAlreadyRegistered(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	first := postRegister(t, handler, url.Values{"email": {"carol@example.com"}, "password": {"correct-password"}})
	if first.Code == http.StatusBadRequest || first.Code == http.StatusConflict {
		t.Fatalf("first registration failed: status = %d, body: %s", first.Code, first.Body.String())
	}

	second := postRegister(t, handler, url.Values{"email": {"Carol@Example.com"}, "password": {"another-password"}})
	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", second.Code, http.StatusConflict)
	}
	if !strings.Contains(second.Body.String(), "already registered") {
		t.Fatalf("body = %q, want an already-registered error", second.Body.String())
	}
}

func TestRegisterWithSignupDisabledReturnsClosedMessageNotFound(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t, accounts.WithSignupDisabled())

	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/accounts/register/", nil))
	if getResponse.Code == http.StatusNotFound {
		t.Fatal("GET /accounts/register/ with signup disabled returned 404, want a closed-registration message")
	}
	if !strings.Contains(getResponse.Body.String(), "closed") {
		t.Fatalf("GET body = %q, want a closed-registration message", getResponse.Body.String())
	}

	postResponse := httptest.NewRecorder()
	handler.ServeHTTP(postResponse, httptest.NewRequest(http.MethodPost, "/accounts/register/", strings.NewReader("email=x@example.com&password=correct-password")))
	if postResponse.Code == http.StatusNotFound {
		t.Fatal("POST /accounts/register/ with signup disabled returned 404, want a closed-registration message")
	}
}

func TestRegisterMissingCSRFTokenIsRejected(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	request := httptest.NewRequest(http.MethodPost, "/accounts/register/", strings.NewReader("email=dave@example.com&password=correct-password"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

// buildRegisterTestHandlerConcurrent is buildRegisterTestHandler over a
// database several connections share, needed to exercise the real race two
// concurrent registrations for the same email create. A PostgreSQL run's
// database already is one. On SQLite it asks for a file explicitly, since
// an in-memory database is limited to one connection.
func buildRegisterTestHandlerConcurrent(t *testing.T) http.Handler {
	t.Helper()

	var store *db.Store
	if testdb.Dialect() == db.SQLite {
		store = sharedSQLiteFileStore(t)
	} else {
		_, store = migratedAccountsDB(t)
	}
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(store)); err != nil {
		t.Fatalf("register accounts app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

// sharedSQLiteFileStore returns a Store over a SQLite file under the test's
// temporary directory, with the accounts tables migrated, that several
// connections can use at once.
func sharedSQLiteFileStore(t *testing.T) *db.Store {
	t.Helper()
	// busy_timeout makes a connection wait for a held write lock instead of
	// immediately erroring with SQLITE_BUSY — needed so this test's real
	// concurrent connections exercise the actual application-level
	// duplicate-email race rather than an unrelated SQLite file-locking
	// error under contention.
	dbPath := filepath.Join(t.TempDir(), "accounts.db")
	sqlDB, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	migrationtest.Apply(t, sqlDB, db.SQLite, accountsModels(t))
	return db.NewStore(sqlDB, db.SQLite)
}

func TestConcurrentRegistrationsForSameEmailNeverBothSucceed(t *testing.T) {
	handler := buildRegisterTestHandlerConcurrent(t)

	const attempts = 8
	responses := make([]*httptest.ResponseRecorder, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses[i] = postRegister(t, handler, url.Values{"email": {"race@example.com"}, "password": {"correct-password"}})
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, r := range responses {
		if r.Code != http.StatusBadRequest && r.Code != http.StatusConflict {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent registrations for the same email = %d, want exactly 1", successes)
	}
}

func TestRegisterIsRateLimitedAfterRepeatedFailures(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = postRegister(t, handler, url.Values{"email": {"eve@example.com"}, "password": {"short"}})
	}

	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status after repeated failures = %d, want %d", last.Code, http.StatusTooManyRequests)
	}
}
