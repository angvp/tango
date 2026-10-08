package accounts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/mail"
	"github.com/angvp/tango/mail/mailtest"
	"github.com/angvp/tango/model"
)

// mailSite is the accounts app with mail enabled, a capturing sender and
// logger, and a clock the test moves.
type mailSite struct {
	t          *testing.T
	handler    http.Handler
	store      *db.Store
	sender     *mailtest.Sender
	logs       *capturedLogs
	lifecycles []tango.Lifecycle

	mu  sync.Mutex
	now time.Time
}

const testBaseURL = "https://example.com"

// newMailSite builds the site. start says whether to start the outbox's
// worker now.
func newMailSite(t *testing.T, sender mail.Sender, start bool, opts ...accounts.Option) *mailSite {
	t.Helper()
	logger, logs := newCapturedLogger()
	site := &mailSite{t: t, logs: logs, now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	if s, ok := sender.(*mailtest.Sender); ok {
		site.sender = s
	}
	opts = append([]accounts.Option{
		accounts.WithMail(accounts.MailConfig{Sender: sender, From: "Shop <noreply@example.com>", BaseURL: testBaseURL, Logger: logger}),
		accounts.WithClock(site.clock),
	}, opts...)
	_, site.store = migratedAccountsDB(t)
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(site.store, opts...)); err != nil {
		t.Fatal(err)
	}
	// A host app with a page only a logged-in account may see.
	private := func(ctx *tango.Context) error { return ctx.JSON(http.StatusOK, map[string]string{"page": "private"}) }
	if err := registry.Register(tango.NewApp("host", func(r *tango.Registry) error {
		return r.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/private/", accounts.RequireLogin(site.store, accounts.DefaultSessionCookieName, "/accounts/login/", private)),
			tango.Path(http.MethodGet, "/verified/", accounts.RequireVerified(site.store, accounts.DefaultSessionCookieName, "/accounts/login/", private)),
		})
	})); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	site.handler = handler
	site.lifecycles = registry.Lifecycles()
	if start {
		site.start()
	}
	t.Cleanup(func() { site.stop() })
	return site
}

func (s *mailSite) clock() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

func (s *mailSite) advance(d time.Duration) {
	s.mu.Lock()
	s.now = s.now.Add(d)
	s.mu.Unlock()
}

func (s *mailSite) start() {
	s.t.Helper()
	for _, l := range s.lifecycles {
		if l.Start != nil {
			if err := l.Start(context.Background()); err != nil {
				s.t.Fatal(err)
			}
		}
	}
}

// stop stops the outbox, which first sends what's queued.
func (s *mailSite) stop() {
	for _, l := range s.lifecycles {
		if l.Stop != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = l.Stop(ctx)
			cancel()
		}
	}
	s.lifecycles = nil
}

// sent waits for the sender to have received n messages, then returns them.
func (s *mailSite) sent(n int) []mail.Message {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		messages := s.sender.Messages()
		if len(messages) >= n || time.Now().After(deadline) {
			if len(messages) != n {
				s.t.Fatalf("sent %d messages, want %d: %+v", len(messages), n, messages)
			}
			return messages
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// createAccount inserts an account directly, as an operator might, with
// the password "old-password".
func (s *mailSite) createAccount(email string, active bool) accounts.Account {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	hash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.MinCost)
	if err != nil {
		s.t.Fatal(err)
	}
	account := accounts.Account{Email: email, PasswordHash: string(hash), Active: active, CreatedAt: s.clock()}
	if err := s.store.Create(context.Background(), meta, &account); err != nil {
		s.t.Fatal(err)
	}
	return account
}

func (s *mailSite) tokens() []accounts.AccountToken {
	s.t.Helper()
	_, meta := accountMetas(s.t)
	var rows []accounts.AccountToken
	if err := s.store.List(context.Background(), meta, db.Query{}, &rows); err != nil {
		s.t.Fatal(err)
	}
	return rows
}

// form GETs path for its CSRF cookie, then POSTs form to it.
func (s *mailSite) postForm(path string, form url.Values, modify ...func(*http.Request)) *httptest.ResponseRecorder {
	s.t.Helper()
	get := httptest.NewRecorder()
	s.handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, path, nil))
	var csrf *http.Cookie
	for _, c := range get.Result().Cookies() {
		if c.Name == "tango_account_pre_session_csrf" {
			csrf = c
		}
	}
	if csrf == nil {
		s.t.Fatalf("GET %s set no CSRF cookie (status %d)", path, get.Code)
	}
	submitted := url.Values{"csrf_token": {csrf.Value}}
	for k, v := range form {
		submitted[k] = v
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(submitted.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(csrf)
	for _, m := range modify {
		m(request)
	}
	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)
	return response
}

func (s *mailSite) askForReset(email string, modify ...func(*http.Request)) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.postForm("/accounts/password-reset/", url.Values{"email": {email}}, modify...)
}

var linkToken = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

// tokenIn returns the token in message's link, which must start with
// prefix.
func tokenIn(t *testing.T, message mail.Message, prefix string) string {
	t.Helper()
	if !strings.Contains(message.Text, prefix+"?token=") {
		t.Fatalf("message text has no link starting %q:\n%s", prefix, message.Text)
	}
	match := linkToken.FindStringSubmatch(message.Text)
	return match[1]
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// capturedLogs is an slog.Handler keeping every record.
type capturedLogs struct {
	mu      sync.Mutex
	records []capturedLog
}

type capturedLog struct {
	message string
	attrs   map[string]string
}

func newCapturedLogger() (*slog.Logger, *capturedLogs) {
	logs := &capturedLogs{}
	return slog.New(logs), logs
}

func (h *capturedLogs) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturedLogs) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *capturedLogs) WithGroup(string) slog.Handler            { return h }
func (h *capturedLogs) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.String(); return true })
	h.mu.Lock()
	h.records = append(h.records, capturedLog{r.Message, attrs})
	h.mu.Unlock()
	return nil
}

func (h *capturedLogs) byMessage(message string) []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []capturedLog
	for _, r := range h.records {
		if r.message == message {
			out = append(out, r)
		}
	}
	return out
}

// accountMetas returns the Account and AccountToken model metadata.
func accountMetas(t *testing.T) (account, token model.ModelMeta) {
	t.Helper()
	for _, m := range accountsModels(t) {
		switch m.Name {
		case "Account":
			account = m
		case "AccountToken":
			token = m
		}
	}
	return account, token
}

// get GETs path with any cookies given.
func (s *mailSite) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	s.t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		if c != nil {
			request.AddCookie(c)
		}
	}
	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)
	return response
}

// logIn logs email in with password and returns the session cookie, or nil
// when the login fails.
func (s *mailSite) logIn(email, password string) *http.Cookie {
	s.t.Helper()
	response := s.postForm("/accounts/login/", url.Values{"email": {email}, "password": {password}}, func(r *http.Request) { r.RemoteAddr = "198.51.100.1:1" })
	for _, c := range response.Result().Cookies() {
		if c.Name == accounts.DefaultSessionCookieName && c.Value != "" {
			return c
		}
	}
	return nil
}

// resetToken asks for a reset for email and returns the emailed token.
func (s *mailSite) resetToken(email string) string {
	s.t.Helper()
	before := len(s.sender.Messages())
	s.askForReset(email, func(r *http.Request) { r.RemoteAddr = "203.0.113.1:1" })
	return tokenIn(s.t, s.sent(before + 1)[before], testBaseURL+"/accounts/password-reset/confirm/")
}

// insertToken stores a token row directly, for cases no flow produces.
func (s *mailSite) insertToken(account accounts.Account, token string, purpose accounts.TokenPurpose, expires time.Time) {
	s.t.Helper()
	_, meta := accountMetas(s.t)
	row := accounts.AccountToken{TokenHash: sha256Hex(token), AccountID: account.ID, Purpose: purpose, AddressHash: sha256Hex(account.Email), ExpiresAt: expires}
	if err := s.store.Create(context.Background(), meta, &row); err != nil {
		s.t.Fatal(err)
	}
}

// updateAccount applies change to email's account, as an operator might.
func (s *mailSite) updateAccount(id int64, change func(*accounts.Account)) {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	var account accounts.Account
	if err := s.store.Get(context.Background(), meta, id, &account); err != nil {
		s.t.Fatal(err)
	}
	change(&account)
	if err := s.store.Update(context.Background(), meta, &account); err != nil {
		s.t.Fatal(err)
	}
}

func (s *mailSite) account(id int64) accounts.Account {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	var account accounts.Account
	if err := s.store.Get(context.Background(), meta, id, &account); err != nil {
		s.t.Fatal(err)
	}
	return account
}

// tokenPageHeaders checks a page carrying a token keeps it out of
// referrers and caches.
func tokenPageHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Referrer-Policy = %q, Cache-Control = %q; want no-referrer and no-store", response.Header().Get("Referrer-Policy"), response.Header().Get("Cache-Control"))
	}
}

func t0() context.Context { return context.Background() }

func dbWhereEmail(email string) db.Query {
	return db.Query{Where: []db.Condition{{Field: "Email", Op: db.OpEq, Value: email}}}
}
