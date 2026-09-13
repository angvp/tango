package admin_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

// TestAdminSecurityJourney exercises the full Milestone 12 security
// surface as one connected flow, rather than only per-ticket unit tests
// in isolation: account creation, login, an authenticated CRUD round
// trip (including its CSRF token), logout, that the old session is truly
// dead afterward, and that CSRF/auth rejections behave as designed along
// the way. This is the milestone's ticket 06 deliverable.
func TestAdminSecurityJourney(t *testing.T) {
	type widget struct {
		ID   int64 `tango:"pk"`
		Name string
	}

	registry := tango.NewRegistry()
	if err := registry.Models().Register(widget{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(widget{}, admin.Options{ListDisplay: []string{"Name"}}); err != nil {
		t.Fatalf("register admin model: %v", err)
	}

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if _, err := sqlDB.Exec(`CREATE TABLE widget (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create widget table: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE admin_user (
		id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL, active BOOLEAN NOT NULL, created_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_user: %v", err)
	}
	if _, err := sqlDB.Exec(`CREATE TABLE admin_session (
		id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL, expires_at TIMESTAMP NOT NULL
	)`); err != nil {
		t.Fatalf("create admin_session: %v", err)
	}
	store := db.NewStore(sqlDB, db.SQLite)

	// 1. Account creation goes through the CLI path, hashing via bcrypt —
	// never a raw password write.
	if err := admin.CreateAccount(context.Background(), store, "alice", "s3cret-pw"); err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}

	// 2. Unauthenticated access is redirected to login with next, never a
	// bare 401/200.
	unauth := httptest.NewRequest(http.MethodGet, "/admin/widget/", nil)
	unauthResp := httptest.NewRecorder()
	handler.ServeHTTP(unauthResp, unauth)
	if unauthResp.Code != http.StatusFound || !strings.HasPrefix(unauthResp.Header().Get("Location"), "/admin/login/?next=") {
		t.Fatalf("unauthenticated request = %d %q, want redirect to login with next", unauthResp.Code, unauthResp.Header().Get("Location"))
	}

	// 3. A wrong password is rejected generically, with no session issued.
	wrongLogin := postLogin(t, handler, url.Values{"username": {"alice"}, "password": {"wrong"}})
	if wrongLogin.Code != http.StatusUnauthorized || len(wrongLogin.Result().Cookies()) != 0 {
		t.Fatalf("wrong-password login = %d, cookies=%v; want 401, no cookies", wrongLogin.Code, wrongLogin.Result().Cookies())
	}

	// 4. The correct login succeeds and issues a session.
	login := postLogin(t, handler, url.Values{"username": {"alice"}, "password": {"s3cret-pw"}})
	if login.Code != http.StatusFound {
		t.Fatalf("correct login = %d, want %d", login.Code, http.StatusFound)
	}
	var session *http.Cookie
	for _, c := range login.Result().Cookies() {
		if c.Name == "tango_admin_session" {
			session = c
		}
	}
	if session == nil {
		t.Fatal("login did not set a session cookie")
	}
	sessionCSRFSum := sha256.Sum256([]byte(session.Value))
	sessionCSRF := hex.EncodeToString(sessionCSRFSum[:])

	// 5. A create POST without the CSRF token is rejected, and creates
	// nothing.
	badCreate := httptest.NewRequest(http.MethodPost, "/admin/widget/new/", strings.NewReader(url.Values{"Name": {"Gadget"}}.Encode()))
	badCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badCreate.AddCookie(session)
	badCreateResp := httptest.NewRecorder()
	handler.ServeHTTP(badCreateResp, badCreate)
	if badCreateResp.Code != http.StatusForbidden {
		t.Fatalf("create without CSRF = %d, want %d", badCreateResp.Code, http.StatusForbidden)
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM widget`).Scan(&count); err != nil {
		t.Fatalf("count widgets: %v", err)
	}
	if count != 0 {
		t.Fatalf("widget count after rejected create = %d, want 0", count)
	}

	// 6. A create POST with the correct CSRF token succeeds.
	goodCreate := httptest.NewRequest(http.MethodPost, "/admin/widget/new/", strings.NewReader(url.Values{"Name": {"Gadget"}, "csrf_token": {sessionCSRF}}.Encode()))
	goodCreate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	goodCreate.AddCookie(session)
	goodCreateResp := httptest.NewRecorder()
	handler.ServeHTTP(goodCreateResp, goodCreate)
	if goodCreateResp.Code != http.StatusFound {
		t.Fatalf("create with CSRF = %d, want %d, body: %s", goodCreateResp.Code, http.StatusFound, goodCreateResp.Body.String())
	}
	var widgetID int64
	if err := sqlDB.QueryRow(`SELECT id FROM widget WHERE name = ?`, "Gadget").Scan(&widgetID); err != nil {
		t.Fatalf("find created widget: %v", err)
	}

	// 7. Deleting it requires POST and the CSRF token; a bare GET to the
	// delete path only renders the confirmation page without deleting.
	confirmGet := httptest.NewRequest(http.MethodGet, deletePath(widgetID), nil)
	confirmGet.AddCookie(session)
	confirmResp := httptest.NewRecorder()
	handler.ServeHTTP(confirmResp, confirmGet)
	if confirmResp.Code != http.StatusOK {
		t.Fatalf("GET delete confirmation = %d, want %d", confirmResp.Code, http.StatusOK)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM widget WHERE id = ?`, widgetID).Scan(&count); err != nil {
		t.Fatalf("count after GET confirm: %v", err)
	}
	if count != 1 {
		t.Fatal("GET to the delete confirmation page deleted the row")
	}

	deletePost := httptest.NewRequest(http.MethodPost, deletePath(widgetID), strings.NewReader(url.Values{"csrf_token": {sessionCSRF}}.Encode()))
	deletePost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deletePost.AddCookie(session)
	deleteResp := httptest.NewRecorder()
	handler.ServeHTTP(deleteResp, deletePost)
	if deleteResp.Code != http.StatusFound {
		t.Fatalf("delete POST = %d, want %d", deleteResp.Code, http.StatusFound)
	}
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM widget WHERE id = ?`, widgetID).Scan(&count); err != nil {
		t.Fatalf("count after delete: %v", err)
	}
	if count != 0 {
		t.Fatal("widget row still exists after a confirmed delete")
	}

	// 8. Logout kills the session; the same cookie no longer authenticates
	// anywhere, including for further destructive actions.
	logout := httptest.NewRequest(http.MethodPost, "/admin/logout/", nil)
	logout.AddCookie(session)
	logoutResp := httptest.NewRecorder()
	handler.ServeHTTP(logoutResp, logout)
	if logoutResp.Code != http.StatusFound {
		t.Fatalf("logout = %d, want %d", logoutResp.Code, http.StatusFound)
	}

	afterLogout := httptest.NewRequest(http.MethodGet, "/admin/widget/", nil)
	afterLogout.AddCookie(session)
	afterLogoutResp := httptest.NewRecorder()
	handler.ServeHTTP(afterLogoutResp, afterLogout)
	if afterLogoutResp.Code != http.StatusFound || !strings.HasPrefix(afterLogoutResp.Header().Get("Location"), "/admin/login/") {
		t.Fatalf("request with logged-out session = %d %q, want redirect to login", afterLogoutResp.Code, afterLogoutResp.Header().Get("Location"))
	}
}

func deletePath(id int64) string {
	return fmt.Sprintf("/admin/widget/%d/delete/", id)
}
