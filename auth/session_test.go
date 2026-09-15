package auth_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	_ "modernc.org/sqlite"
)

type authTestUser struct {
	ID           int64 `tango:"pk"`
	Email        string
	PasswordHash string
}

type authTestSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=authTestUser,index"`
	ExpiresAt time.Time
}

type authSessionMissingToken struct {
	ID        int64 `tango:"pk"`
	UserID    int64 `tango:"fk=authTestUser"`
	ExpiresAt time.Time
}

type authSessionUserIDNotFK struct {
	ID        int64 `tango:"pk"`
	Token     string
	UserID    int64
	ExpiresAt time.Time
}

type authSessionBadExpiresAt struct {
	ID        int64 `tango:"pk"`
	Token     string
	UserID    int64 `tango:"fk=authTestUser"`
	ExpiresAt string
}

type authSessionTokenNotString struct {
	ID        int64 `tango:"pk"`
	Token     int64
	UserID    int64 `tango:"fk=authTestUser"`
	ExpiresAt time.Time
}

type authSessionMissingUserID struct {
	ID        int64 `tango:"pk"`
	Token     string
	ExpiresAt time.Time
}

type authSessionMissingExpiresAt struct {
	ID     int64 `tango:"pk"`
	Token  string
	UserID int64 `tango:"fk=authTestUser"`
}

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := auth.HashPassword("correct")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "correct" {
		t.Fatalf("hash = %q, want non-empty bcrypt hash", hash)
	}
	if !auth.VerifyPassword(hash, "correct") {
		t.Fatal("VerifyPassword returned false for the correct password")
	}
	if auth.VerifyPassword(hash, "wrong") {
		t.Fatal("VerifyPassword returned true for the wrong password")
	}
	if auth.VerifyPassword("not-a-bcrypt-hash", "correct") {
		t.Fatal("VerifyPassword returned true for a garbled hash")
	}
}

// TestHashPasswordRejectsPasswordsLongerThanBcryptLimit documents a real
// constraint a framework user can hit: bcrypt only examines the first 72
// bytes of its input and refuses longer passwords outright, rather than
// silently truncating them.
func TestHashPasswordRejectsPasswordsLongerThanBcryptLimit(t *testing.T) {
	tooLong := strings.Repeat("a", 73)
	if _, err := auth.HashPassword(tooLong); err == nil {
		t.Fatal("HashPassword err = nil, want an error for a password over bcrypt's 72-byte limit")
	}
}

func TestSessionCreateLookupAndDelete(t *testing.T) {
	store, _, userMeta, sessionMeta, userID := buildAuthTestStore(t)

	token, expiresAt, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if !expiresAt.After(time.Now().UTC()) {
		t.Fatalf("expiresAt = %s, want future", expiresAt)
	}
	assertCookieSafeToken(t, token)

	gotUserID, ok, err := auth.SessionUser(context.Background(), store, sessionMeta, token)
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if !ok || gotUserID != userID {
		t.Fatalf("SessionUser = (%v, %v), want (%d, true)", gotUserID, ok, userID)
	}

	var user authTestUser
	if err := store.Get(context.Background(), userMeta, gotUserID, &user); err != nil {
		t.Fatalf("hydrate current user: %v", err)
	}
	if user.Email != "ada@example.test" {
		t.Fatalf("hydrated user email = %q", user.Email)
	}

	if err := auth.DeleteSession(context.Background(), store, sessionMeta, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	_, ok, err = auth.SessionUser(context.Background(), store, sessionMeta, token)
	if err != nil {
		t.Fatalf("SessionUser after delete: %v", err)
	}
	if ok {
		t.Fatal("SessionUser ok = true after DeleteSession")
	}
}

func TestSessionUserFalseForMissingExpiredAndEmptyToken(t *testing.T) {
	store, _, _, sessionMeta, userID := buildAuthTestStore(t)

	if _, ok, err := auth.SessionUser(context.Background(), store, sessionMeta, "missing"); err != nil || ok {
		t.Fatalf("missing SessionUser = ok %v err %v, want false nil", ok, err)
	}
	if _, ok, err := auth.SessionUser(context.Background(), store, sessionMeta, ""); err != nil || ok {
		t.Fatalf("empty SessionUser = ok %v err %v, want false nil", ok, err)
	}

	token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, -time.Hour)
	if err != nil {
		t.Fatalf("CreateSession expired: %v", err)
	}
	if _, ok, err := auth.SessionUser(context.Background(), store, sessionMeta, token); err != nil || ok {
		t.Fatalf("expired SessionUser = ok %v err %v, want false nil", ok, err)
	}
}

func TestDeleteSessionMissingTokenIsNoop(t *testing.T) {
	store, _, _, sessionMeta, _ := buildAuthTestStore(t)

	if err := auth.DeleteSession(context.Background(), store, sessionMeta, "missing"); err != nil {
		t.Fatalf("DeleteSession missing: %v", err)
	}
	if err := auth.DeleteSession(context.Background(), store, sessionMeta, ""); err != nil {
		t.Fatalf("DeleteSession empty: %v", err)
	}
}

func TestCreateSessionUserIDAssignment(t *testing.T) {
	t.Run("nil userID is rejected", func(t *testing.T) {
		store, _, _, sessionMeta, _ := buildAuthTestStore(t)
		_, _, err := auth.CreateSession(context.Background(), store, sessionMeta, nil, time.Hour)
		if !errors.Is(err, auth.ErrInvalidSessionModel) {
			t.Fatalf("CreateSession(nil userID) error = %v, want ErrInvalidSessionModel", err)
		}
	})

	t.Run("convertible userID type is accepted", func(t *testing.T) {
		store, _, _, sessionMeta, userID := buildAuthTestStore(t)
		// UserID is int64; int32 is convertible to it even though it is not
		// directly assignable, exercising setReflectValue's ConvertibleTo path.
		token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, int32(userID), time.Hour)
		if err != nil {
			t.Fatalf("CreateSession(int32 userID): %v", err)
		}
		gotUserID, ok, err := auth.SessionUser(context.Background(), store, sessionMeta, token)
		if err != nil || !ok {
			t.Fatalf("SessionUser after convertible userID = (%v, %v, %v), want a valid session", gotUserID, ok, err)
		}
	})

	t.Run("inconvertible userID type is rejected", func(t *testing.T) {
		store, _, _, sessionMeta, _ := buildAuthTestStore(t)
		_, _, err := auth.CreateSession(context.Background(), store, sessionMeta, struct{ X int }{X: 1}, time.Hour)
		if !errors.Is(err, auth.ErrInvalidSessionModel) {
			t.Fatalf("CreateSession(struct userID) error = %v, want ErrInvalidSessionModel", err)
		}
	})
}

func TestSessionHelpersPropagateUnderlyingStoreErrors(t *testing.T) {
	t.Run("CreateSession", func(t *testing.T) {
		store, sqlDB, _, sessionMeta, userID := buildAuthTestStore(t)
		sqlDB.Close()
		if _, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour); err == nil {
			t.Fatal("CreateSession over a closed database returned nil error, want a store error")
		}
	})

	t.Run("SessionUser", func(t *testing.T) {
		store, sqlDB, _, sessionMeta, userID := buildAuthTestStore(t)
		token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		sqlDB.Close()
		if _, _, err := auth.SessionUser(context.Background(), store, sessionMeta, token); err == nil {
			t.Fatal("SessionUser over a closed database returned nil error, want a store error")
		}
	})

	t.Run("DeleteSession query", func(t *testing.T) {
		store, sqlDB, _, sessionMeta, userID := buildAuthTestStore(t)
		token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		sqlDB.Close()
		if err := auth.DeleteSession(context.Background(), store, sessionMeta, token); err == nil {
			t.Fatal("DeleteSession over a closed database returned nil error, want a store error")
		}
	})

	t.Run("DeleteSession delete", func(t *testing.T) {
		store, sqlDB, _, sessionMeta, userID := buildAuthTestStore(t)
		token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		// Enforce foreign keys on this connection and add a row that
		// references the session, so the session row's SELECT (inside
		// DeleteSession) succeeds but the subsequent DELETE is rejected by
		// the FK constraint — a realistic way an app-level FK could make a
		// session undeletable without the token itself being invalid.
		sqlDB.SetMaxOpenConns(1)
		if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
			t.Fatalf("enable foreign keys: %v", err)
		}
		if _, err := sqlDB.Exec(`CREATE TABLE auth_test_session_dependent (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id INTEGER NOT NULL REFERENCES auth_test_session(id)
		)`); err != nil {
			t.Fatalf("create dependent table: %v", err)
		}
		var sessionID int64
		if err := sqlDB.QueryRow("SELECT id FROM auth_test_session WHERE token = ?", token).Scan(&sessionID); err != nil {
			t.Fatalf("look up session id: %v", err)
		}
		if _, err := sqlDB.Exec("INSERT INTO auth_test_session_dependent (session_id) VALUES (?)", sessionID); err != nil {
			t.Fatalf("insert dependent row: %v", err)
		}

		if err := auth.DeleteSession(context.Background(), store, sessionMeta, token); err == nil {
			t.Fatal("DeleteSession violating a foreign key constraint returned nil error, want a store error")
		}
	})
}

func TestValidateSessionModelFailures(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "missing token", value: authSessionMissingToken{}},
		{name: "userid not foreign key", value: authSessionUserIDNotFK{}},
		{name: "bad expires at", value: authSessionBadExpiresAt{}},
		{name: "token not string", value: authSessionTokenNotString{}},
		{name: "missing userid", value: authSessionMissingUserID{}},
		{name: "missing expires at", value: authSessionMissingExpiresAt{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := mustRegisterModel(t, tt.value)
			err := auth.ValidateSessionModel(meta)
			if !errors.Is(err, auth.ErrInvalidSessionModel) {
				t.Fatalf("ValidateSessionModel error = %v, want ErrInvalidSessionModel", err)
			}

			store, _, _, _, userID := buildAuthTestStore(t)
			if _, _, err := auth.CreateSession(context.Background(), store, meta, userID, time.Hour); !errors.Is(err, auth.ErrInvalidSessionModel) {
				t.Fatalf("CreateSession invalid error = %v, want ErrInvalidSessionModel", err)
			}
			if _, _, err := auth.SessionUser(context.Background(), store, meta, "token"); !errors.Is(err, auth.ErrInvalidSessionModel) {
				t.Fatalf("SessionUser invalid error = %v, want ErrInvalidSessionModel", err)
			}
			if err := auth.DeleteSession(context.Background(), store, meta, "token"); !errors.Is(err, auth.ErrInvalidSessionModel) {
				t.Fatalf("DeleteSession invalid error = %v, want ErrInvalidSessionModel", err)
			}
		})
	}
}

func TestValidateSessionModelMissingPrimaryKey(t *testing.T) {
	meta := model.ModelMeta{
		Name: "ManualBadSession",
		Fields: []model.FieldMeta{
			{Name: "Token", Type: reflectTypeOf[string]()},
			{Name: "UserID", Type: reflectTypeOf[int64](), ForeignKey: "authTestUser"},
			{Name: "ExpiresAt", Type: reflectTypeOf[time.Time]()},
		},
	}
	if err := auth.ValidateSessionModel(meta); !errors.Is(err, auth.ErrInvalidSessionModel) {
		t.Fatalf("ValidateSessionModel missing pk = %v, want ErrInvalidSessionModel", err)
	}
}

func TestRequireLoginAndCurrentUserID(t *testing.T) {
	store, _, userMeta, sessionMeta, userID := buildAuthTestStore(t)
	token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var hydratedEmail string
	protected := auth.RequireLogin(store, sessionMeta, "app_session", "/login/", "/app/", func(ctx *tango.Context) error {
		currentUserID, ok, err := auth.CurrentUserID(ctx, store, sessionMeta, "app_session")
		if err != nil {
			return err
		}
		if !ok {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "missing user"})
		}
		var user authTestUser
		if err := store.Get(ctx.Context(), userMeta, currentUserID, &user); err != nil {
			return err
		}
		hydratedEmail = user.Email
		return ctx.JSON(http.StatusOK, map[string]string{"email": user.Email})
	})
	handler := buildAuthHTTPHandler(t, protected)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/app/private/?tab=1", nil))
	if unauthenticated.Code != http.StatusFound {
		t.Fatalf("unauthenticated status = %d, want 302", unauthenticated.Code)
	}
	location := unauthenticated.Header().Get("Location")
	if location != "/login/?next=%2Fapp%2Fprivate%2F%3Ftab%3D1" {
		t.Fatalf("Location = %q, want login with safe next", location)
	}

	authenticatedReq := httptest.NewRequest(http.MethodGet, "/app/private/", nil)
	authenticatedReq.AddCookie(&http.Cookie{Name: "app_session", Value: token})
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, authenticatedReq)
	if authenticated.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200 body %s", authenticated.Code, authenticated.Body.String())
	}
	if hydratedEmail != "ada@example.test" {
		t.Fatalf("hydrated email = %q", hydratedEmail)
	}
}

func TestRequireLoginPropagatesCurrentUserIDError(t *testing.T) {
	store, sqlDB, _, sessionMeta, userID := buildAuthTestStore(t)
	token, _, err := auth.CreateSession(context.Background(), store, sessionMeta, userID, time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	// Close the database out from under a valid cookie so CurrentUserID's
	// session lookup fails with a real error instead of "not found".
	sqlDB.Close()

	protected := auth.RequireLogin(store, sessionMeta, "app_session", "/login/", "/app/", func(ctx *tango.Context) error {
		t.Fatal("protected view ran, want RequireLogin to propagate the lookup error first")
		return nil
	})
	handler := buildAuthHTTPHandler(t, protected)

	req := httptest.NewRequest(http.MethodGet, "/app/private/", nil)
	req.AddCookie(&http.Cookie{Name: "app_session", Value: token})
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 for a propagated store error", resp.Code)
	}
}

func TestRequireLoginFallsBackWhenRequestOutsideAllowedPrefix(t *testing.T) {
	store, _, _, sessionMeta, _ := buildAuthTestStore(t)
	handler := buildAuthHTTPHandler(t, auth.RequireLogin(store, sessionMeta, "app_session", "/login/", "/app/", func(ctx *tango.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/elsewhere/", nil))
	location := response.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if parsed.Path != "/login/" || parsed.Query().Get("next") != "/app/" {
		t.Fatalf("Location = %q, want /login/?next=/app/", location)
	}
}

// TestRequireLoginFallsBackToRawLoginPathWhenItFailsToParse documents
// appendNext's defensive fallback: an unparsable loginPath (a configuration
// mistake, not something the request can trigger) is redirected to as-is,
// without a "next" query parameter appended, rather than panicking or
// silently dropping the redirect.
func TestRequireLoginFallsBackToRawLoginPathWhenItFailsToParse(t *testing.T) {
	store, _, _, sessionMeta, _ := buildAuthTestStore(t)
	handler := buildAuthHTTPHandler(t, auth.RequireLogin(store, sessionMeta, "app_session", "/login%zz", "/app/", func(ctx *tango.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "true"})
	}))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app/private/", nil))
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if got := response.Header().Get("Location"); got != "/login%zz" {
		t.Fatalf("Location = %q, want the raw unparsable loginPath unchanged", got)
	}
}

func TestApplicationAuthFullLoginProtectCurrentUserLogoutFlow(t *testing.T) {
	store, _, userMeta, sessionMeta, _ := buildAuthTestStore(t)
	passwordHash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user := authTestUser{Email: "grace@example.test", PasswordHash: passwordHash}
	if err := store.Create(context.Background(), userMeta, &user); err != nil {
		t.Fatalf("create login user: %v", err)
	}

	protected := auth.RequireLogin(store, sessionMeta, "app_session", "/login/", "/app/", func(ctx *tango.Context) error {
		userID, ok, err := auth.CurrentUserID(ctx, store, sessionMeta, "app_session")
		if err != nil {
			return err
		}
		if !ok {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "missing session"})
		}
		var current authTestUser
		if err := store.Get(ctx.Context(), userMeta, userID, &current); err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, map[string]string{"email": current.Email})
	})

	login := func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		email := ctx.Request().PostForm.Get("email")
		password := ctx.Request().PostForm.Get("password")

		var users []authTestUser
		if err := store.Query(ctx.Context(), &users, "SELECT id, email, password_hash FROM auth_test_user WHERE email = ?", email); err != nil {
			return err
		}
		if len(users) != 1 || !auth.VerifyPassword(users[0].PasswordHash, password) {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		}

		token, expiresAt, err := auth.CreateSession(ctx.Context(), store, sessionMeta, users[0].ID, time.Hour)
		if err != nil {
			return err
		}
		http.SetCookie(ctx.ResponseWriter(), &http.Cookie{
			Name:     "app_session",
			Value:    token,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		target := auth.SafeRedirect(ctx.Request().PostForm.Get("next"), "/app/", "/app/")
		return ctx.Redirect(target)
	}

	logout := func(ctx *tango.Context) error {
		if cookie, err := ctx.Request().Cookie("app_session"); err == nil {
			if err := auth.DeleteSession(ctx.Context(), store, sessionMeta, cookie.Value); err != nil {
				return err
			}
		}
		http.SetCookie(ctx.ResponseWriter(), &http.Cookie{
			Name:     "app_session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})
		return ctx.Redirect("/login/")
	}

	handler := buildAuthFlowHTTPHandler(t, login, logout, protected)

	loggedOut := performAuthFlowRequest(handler, http.MethodGet, "/app/private/", nil, nil)
	if loggedOut.Code != http.StatusFound {
		t.Fatalf("logged-out protected status = %d, want 302", loggedOut.Code)
	}
	if got := loggedOut.Header().Get("Location"); got != "/login/?next=%2Fapp%2Fprivate%2F" {
		t.Fatalf("logged-out Location = %q, want login next", got)
	}

	wrongLogin := performAuthFlowRequest(handler, http.MethodPost, "/login/", url.Values{
		"email":    {"grace@example.test"},
		"password": {"wrong"},
		"next":     {"/app/private/"},
	}, nil)
	if wrongLogin.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login status = %d, want 401", wrongLogin.Code)
	}
	if len(wrongLogin.Result().Cookies()) != 0 {
		t.Fatalf("wrong login cookies = %+v, want none", wrongLogin.Result().Cookies())
	}

	loginResp := performAuthFlowRequest(handler, http.MethodPost, "/login/", url.Values{
		"email":    {"grace@example.test"},
		"password": {"secret"},
		"next":     {"/app/private/"},
	}, nil)
	if loginResp.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302 body %s", loginResp.Code, loginResp.Body.String())
	}
	if got := loginResp.Header().Get("Location"); got != "/app/private/" {
		t.Fatalf("login Location = %q, want /app/private/", got)
	}
	sessionCookie := findCookie(t, loginResp.Result().Cookies(), "app_session")
	if sessionCookie.Value == "" {
		t.Fatal("login session cookie is empty")
	}

	loggedIn := performAuthFlowRequest(handler, http.MethodGet, "/app/private/", nil, sessionCookie)
	if loggedIn.Code != http.StatusOK {
		t.Fatalf("logged-in protected status = %d, want 200 body %s", loggedIn.Code, loggedIn.Body.String())
	}
	if !strings.Contains(loggedIn.Body.String(), "grace@example.test") {
		t.Fatalf("protected body does not contain hydrated current user email: %s", loggedIn.Body.String())
	}

	logoutResp := performAuthFlowRequest(handler, http.MethodPost, "/logout/", nil, sessionCookie)
	if logoutResp.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want 302", logoutResp.Code)
	}
	if got := logoutResp.Header().Get("Location"); got != "/login/" {
		t.Fatalf("logout Location = %q, want /login/", got)
	}
	cleared := findCookie(t, logoutResp.Result().Cookies(), "app_session")
	if cleared.MaxAge != -1 {
		t.Fatalf("logout cookie MaxAge = %d, want -1", cleared.MaxAge)
	}

	afterLogout := performAuthFlowRequest(handler, http.MethodGet, "/app/private/", nil, sessionCookie)
	if afterLogout.Code != http.StatusFound {
		t.Fatalf("after logout protected status = %d, want 302", afterLogout.Code)
	}
	if got := afterLogout.Header().Get("Location"); got != "/login/?next=%2Fapp%2Fprivate%2F" {
		t.Fatalf("after logout Location = %q, want login next", got)
	}
}

func buildAuthTestStore(t *testing.T) (*db.Store, *sql.DB, model.ModelMeta, model.ModelMeta, int64) {
	t.Helper()

	models := model.NewRegistry()
	if err := models.Register(authTestUser{}); err != nil {
		t.Fatalf("register user: %v", err)
	}
	if err := models.Register(authTestSession{}); err != nil {
		t.Fatalf("register session: %v", err)
	}
	userMeta, _ := models.Get("authTestUser")
	sessionMeta, _ := models.Get("authTestSession")

	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, stmt := range []string{
		`CREATE TABLE auth_test_user (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL, password_hash TEXT NOT NULL)`,
		`CREATE TABLE auth_test_session (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT NOT NULL UNIQUE, user_id INTEGER NOT NULL, expires_at TIMESTAMP NOT NULL)`,
	} {
		if _, err := sqlDB.Exec(stmt); err != nil {
			t.Fatalf("create table: %v", err)
		}
	}
	store := db.NewStore(sqlDB, db.SQLite)
	user := authTestUser{Email: "ada@example.test", PasswordHash: "hash"}
	if err := store.Create(context.Background(), userMeta, &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return store, sqlDB, userMeta, sessionMeta, user.ID
}

func mustRegisterModel(t *testing.T, value any) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	if err := registry.Register(value); err != nil {
		t.Fatalf("register %T: %v", value, err)
	}
	meta, _ := registry.Get(reflectTypeName(value))
	return meta
}

func buildAuthHTTPHandler(t *testing.T, protected tango.View) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Register(tango.NewApp("auth-test", func(reg *tango.Registry) error {
		return reg.Routes().Include("", tango.URLs{
			tango.Path(http.MethodGet, "/app/private/", protected),
			tango.Path(http.MethodGet, "/elsewhere/", protected),
		})
	})); err != nil {
		t.Fatalf("register app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

func buildAuthFlowHTTPHandler(t *testing.T, login tango.View, logout tango.View, protected tango.View) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Register(tango.NewApp("auth-flow-test", func(reg *tango.Registry) error {
		return reg.Routes().Include("", tango.URLs{
			tango.Path(http.MethodPost, "/login/", login),
			tango.Path(http.MethodPost, "/logout/", logout),
			tango.Path(http.MethodGet, "/app/private/", protected),
		})
	})); err != nil {
		t.Fatalf("register app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return handler
}

func performAuthFlowRequest(handler http.Handler, method string, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %+v", name, cookies)
	return nil
}

func assertCookieSafeToken(t *testing.T, token string) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token is not raw URL base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded token length = %d, want 32", len(raw))
	}
	if strings.ContainsAny(token, "+/=") {
		t.Fatalf("token %q contains characters requiring cookie escaping", token)
	}
}

func reflectTypeOf[T any]() reflect.Type {
	var zero T
	return reflect.TypeOf(zero)
}

func reflectTypeName(value any) string {
	t := reflect.TypeOf(value)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
