package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/mail"
	"github.com/angvp/tango/mail/mailtest"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"

	"spa-accounts/migrations"
)

const clientURL = "http://localhost:3000"

// site is the real application, built by appConfig like main, with a sender
// that records emails, driven the way a separate single-page client would:
// JSON requests, bearer tokens, no cookies.
type site struct {
	t       *testing.T
	handler http.Handler
	db      *sql.DB
	dialect db.Dialect
	sender  *mailtest.Sender
}

func newSite(t *testing.T) *site {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sender := &mailtest.Sender{}
	config, err := appConfig(db.NewStore(sqlDB, dialect), sender, settings{
		BaseURL: "http://localhost:8000", ClientURL: clientURL, JWTSecret: []byte(strings.Repeat("s", 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := tango.BuildRegistry(config)
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
	handler = tangoMiddleware(config, handler)
	for _, l := range registry.Lifecycles() {
		if err := l.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Stop(context.Background()) })
	}
	return &site{t: t, handler: handler, db: sqlDB, dialect: dialect, sender: sender}
}

// tangoMiddleware applies config.Middleware around handler the way Serve
// does under MiddlewareScopeAll, so a preflight request reaches CORS.
func tangoMiddleware(config tango.Config, handler http.Handler) http.Handler {
	for i := len(config.Middleware) - 1; i >= 0; i-- {
		handler = config.Middleware[i](handler)
	}
	return handler
}

type response struct {
	*httptest.ResponseRecorder
}

func (r response) json() map[string]any {
	var out map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		panic("not a JSON object: " + r.Body.String())
	}
	return out
}

func (s *site) do(method, path, body string, headers map[string]string) response {
	s.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		request.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, request)
	return response{recorder}
}

func (s *site) post(path, body string, headers ...map[string]string) response {
	s.t.Helper()
	h := map[string]string{}
	for _, extra := range headers {
		for k, v := range extra {
			h[k] = v
		}
	}
	return s.do(http.MethodPost, path, body, h)
}

func bearer(grant map[string]any) map[string]string {
	return map[string]string{"Authorization": "Bearer " + grant["access_token"].(string)}
}

func (s *site) count(table string) int {
	s.t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		s.t.Fatal(err)
	}
	return n
}

// mailed waits for n messages to have been sent and returns them.
func (s *site) mailed(n int) []mail.Message {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		messages := s.sender.Messages()
		if len(messages) >= n || time.Now().After(deadline) {
			if len(messages) != n {
				s.t.Fatalf("%d messages, want %d: %+v", len(messages), n, messages)
			}
			return messages
		}
		time.Sleep(5 * time.Millisecond)
	}
}

var fragmentToken = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)

func tokenIn(t *testing.T, message mail.Message, page string) string {
	t.Helper()
	if !strings.Contains(message.Text, clientURL+page+"#token=") {
		t.Fatalf("the email does not link to the client's %s page:\n%s", page, message.Text)
	}
	return fragmentToken.FindStringSubmatch(message.Text)[1]
}

const registration = `{"email":"Ada@Example.com","password":"correct-horse","profile":{"username":"ada","display_name":"Ada L.","bio":"first programmer"}}`

func TestTheWholeFlowAsASeparateClient(t *testing.T) {
	s := newSite(t)

	// Register: the account and its profile are created together.
	registered := s.post("/accounts/api/register/", registration)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", registered.Code, registered.Body.String())
	}
	grant := registered.json()["grant"].(map[string]any)
	if registered.Header().Get("Set-Cookie") != "" {
		t.Fatal("JSON registration set a cookie")
	}
	me := s.do(http.MethodGet, "/api/me/", "", bearer(grant)).json()
	if me["email"] != "ada@example.com" || me["username"] != "ada" || me["display_name"] != "Ada L." || me["email_verified"] != false {
		t.Fatalf("/api/me/ = %v", me)
	}
	if got := s.do(http.MethodGet, "/api/me/", "", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("/api/me/ without a token: %d", got)
	}

	// Verify through the link the single-page app would open.
	token := tokenIn(t, s.mailed(1)[0], "/verify")
	if got := s.post("/accounts/api/verify/", `{"token":"`+token+`"}`).Code; got != http.StatusNoContent {
		t.Fatalf("verify: %d", got)
	}
	if verified := s.do(http.MethodGet, "/api/me/", "", bearer(grant)).json(); verified["email_verified"] != true {
		t.Fatalf("/api/me/ after verifying = %v", verified)
	}

	// Log in with the email, then with the username.
	for _, identifier := range []string{"ada@example.com", "ada"} {
		login := s.post("/accounts/api/login/", `{"identifier":"`+identifier+`","password":"correct-horse"}`)
		if login.Code != http.StatusOK {
			t.Fatalf("login as %q: %d %s", identifier, login.Code, login.Body.String())
		}
	}

	// Reset the password.
	if got := s.post("/accounts/api/password-reset/", `{"email":"ada@example.com"}`).Code; got != http.StatusAccepted {
		t.Fatalf("reset request: %d", got)
	}
	reset := tokenIn(t, s.mailed(2)[1], "/reset")
	if got := s.post("/accounts/api/password-reset/confirm/", `{"token":"`+reset+`","password":"a-new-password"}`).Code; got != http.StatusNoContent {
		t.Fatalf("reset confirm: %d", got)
	}
	if got := s.post("/accounts/api/login/", `{"identifier":"ada","password":"a-new-password"}`).Code; got != http.StatusOK {
		t.Fatalf("login with the new password: %d", got)
	}
	// Bearer tokens are stateless: the reset did not, and cannot, revoke one.
	if got := s.do(http.MethodGet, "/api/me/", "", bearer(grant)).Code; got != http.StatusOK {
		t.Fatalf("a token issued before the reset: %d", got)
	}
}

func TestBoundedProfileFieldsAreRefusedAndRolledBackWhole(t *testing.T) {
	s := newSite(t)
	tests := []struct {
		name, profile, field string
	}{
		{"a 31 character username", `{"username":"` + strings.Repeat("u", 31) + `"}`, "Username"},
		{"a 61 character display name", `{"username":"ok","display_name":"` + strings.Repeat("d", 61) + `"}`, "DisplayName"},
		{"an @ in the username", `{"username":"a@b"}`, "username"},
		{"no username", `{"bio":"x"}`, "username"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := s.post("/accounts/api/register/", `{"email":"`+strings.NewReplacer(" ", "", "@", "at").Replace(tt.name)+`@example.com","password":"correct-horse","profile":`+tt.profile+`}`)
			fields, _ := r.json()["fields"].(map[string]any)
			if r.Code != http.StatusUnprocessableEntity || r.json()["code"] != "invalid_field" || fields[tt.field] == nil {
				t.Fatalf("status %d: %s; want 422 with fields.%s", r.Code, r.Body.String(), tt.field)
			}
		})
		// Rate-limit headroom: a fresh client key per case is not available
		// in a recorder test, so the budget is the only thing shared.
	}
	if s.count("account") != 0 || s.count("profile") != 0 || len(s.sender.Messages()) != 0 {
		t.Fatalf("a refused registration left %d accounts, %d profiles and %d emails", s.count("account"), s.count("profile"), len(s.sender.Messages()))
	}
}

func TestAnOverLongUsernameLeavesNothingBehind(t *testing.T) {
	s := newSite(t)
	r := s.post("/accounts/api/register/", `{"email":"long@example.com","password":"correct-horse","profile":{"username":"`+strings.Repeat("é", 31)+`"}}`)
	if r.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	if s.count("account") != 0 || s.count("profile") != 0 || len(s.sender.Messages()) != 0 {
		t.Fatal("an over-long username left an account, profile or email behind")
	}
	// Thirty multi-byte characters fit: the limit counts characters, not bytes.
	ok := s.post("/accounts/api/register/", `{"email":"fits@example.com","password":"correct-horse","profile":{"username":"`+strings.Repeat("é", 30)+`"}}`)
	if ok.Code != http.StatusCreated {
		t.Fatalf("30 characters: %d %s", ok.Code, ok.Body.String())
	}
}

func TestAnUnboundedBioAcceptsALongText(t *testing.T) {
	s := newSite(t)
	bio := strings.Repeat("b", 3000)
	r := s.post("/accounts/api/register/", `{"email":"bio@example.com","password":"correct-horse","profile":{"username":"bio","bio":"`+bio+`"}}`)
	if r.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", r.Code, r.Body.String())
	}
	me := s.do(http.MethodGet, "/api/me/", "", bearer(r.json()["grant"].(map[string]any))).json()
	if me["bio"] != bio {
		t.Fatal("the stored bio is not the one sent")
	}
}

func TestTheMigrationGivesTheBoundedColumnsTheirLengths(t *testing.T) {
	s := newSite(t)
	for column, want := range map[string]string{"username": "(30)", "display_name": "(60)"} {
		var declared string
		query := "SELECT type FROM pragma_table_info('profile') WHERE name = $1"
		if s.dialect == db.Postgres {
			query = "SELECT character_maximum_length::text FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'profile' AND column_name = $1"
			want = strings.Trim(want, "()")
		}
		if err := s.db.QueryRow(query, column).Scan(&declared); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(declared), strings.ToLower(want)) {
			t.Fatalf("%s is declared %q, want it to carry %s", column, declared, want)
		}
	}
}

func TestCORSIsTheHostsAndAnswersOnlyTheClientOrigin(t *testing.T) {
	s := newSite(t)
	preflight := s.do(http.MethodOptions, "/accounts/api/login/", "", map[string]string{"Origin": clientURL})
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != clientURL {
		t.Fatalf("preflight: %d %v", preflight.Code, preflight.Header())
	}
	other := s.do(http.MethodOptions, "/accounts/api/login/", "", map[string]string{"Origin": "https://evil.example"})
	if other.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("another origin was allowed")
	}
}
