package accounts_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
)

const (
	testVerifyURL = "https://app.example.com/verify#token={token}"
	testResetURL  = "https://app.example.com/reset#token={token}"
)

// fakeAuth is a host's bearer issuer: its tokens are "tok-<id>".
type fakeAuth struct {
	mu       sync.Mutex
	issueErr error
	issued   []accounts.Account
}

func (a *fakeAuth) Issue(_ context.Context, account accounts.Account) (accounts.AuthGrant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.issueErr != nil {
		return accounts.AuthGrant{}, a.issueErr
	}
	a.issued = append(a.issued, account)
	return accounts.AuthGrant{AccessToken: "tok-" + strconv.FormatInt(account.ID, 10), ExpiresIn: 15 * time.Minute}, nil
}

func (a *fakeAuth) Authenticate(_ context.Context, r *http.Request) (int64, bool, error) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer tok-")
	if !ok {
		return 0, false, nil
	}
	id, err := strconv.ParseInt(token, 10, 64)
	return id, err == nil, nil
}

func (a *fakeAuth) setIssueError(err error) {
	a.mu.Lock()
	a.issueErr = err
	a.mu.Unlock()
}

// newJSONSite is the mail site with JSON mode on.
func newJSONSite(t *testing.T, auth *fakeAuth, opts ...accounts.Option) *mailSite {
	t.Helper()
	return newJSONSiteWith(t, accounts.JSONConfig{Auth: auth}, opts...)
}

// newJSONSiteWith is newJSONSite with the rest of the JSON configuration
// (hooks, a resolver) supplied by the test; the link templates default.
func newJSONSiteWith(t *testing.T, config accounts.JSONConfig, opts ...accounts.Option) *mailSite {
	t.Helper()
	if config.VerifyURL == "" {
		config.VerifyURL = testVerifyURL
	}
	if config.ResetURL == "" {
		config.ResetURL = testResetURL
	}
	opts = append([]accounts.Option{accounts.WithJSON(config)}, opts...)
	return newMailSite(t, &mailtest.Sender{}, true, opts...)
}

// jsonRequest sends body to path with a JSON content type and returns the
// response.
func (s *mailSite) jsonRequest(method, path, body string, modify ...func(*http.Request)) *httptest.ResponseRecorder {
	s.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for _, m := range modify {
		m(request)
	}
	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)
	return response
}

func (s *mailSite) postJSON(path, body string, modify ...func(*http.Request)) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.jsonRequest(http.MethodPost, path, body, modify...)
}

// decode parses a response body into a generic object.
func decode(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
		t.Fatalf("response body is not a JSON object: %v\n%s", err, response.Body.String())
	}
	return out
}

// assertJSONHeaders checks the two headers every JSON-mode response carries.
func assertJSONHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func registrationError(t *testing.T, auth accounts.JSONConfig, withMail bool) error {
	t.Helper()
	opts := []accounts.Option{accounts.WithJSON(auth)}
	if withMail {
		opts = append(opts, accounts.WithMail(accounts.MailConfig{Sender: &mailtest.Sender{}, From: "Shop <noreply@example.com>", BaseURL: testBaseURL}))
	}
	_, store := migratedAccountsDB(t)
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(store, opts...)); err != nil {
		t.Fatal(err)
	}
	return registry.RunRegistration()
}
