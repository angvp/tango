package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
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

	"accounts-mail/migrations"
)

// site is the real application, built by appConfig like main, with a
// sender that records emails, served over HTTP to a browser-like client.
type site struct {
	t      *testing.T
	url    string
	client *http.Client
	sender *mailtest.Sender
}

func newSite(t *testing.T) *site {
	t.Helper()
	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sender := &mailtest.Sender{}
	server := httptest.NewUnstartedServer(nil)
	registry, err := tango.BuildRegistry(appConfig(db.NewStore(sqlDB, dialect), sender, "http://"+server.Listener.Addr().String()))
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
	// The accounts outbox runs as a Lifecycle component, as ServeContext
	// would run it.
	for _, l := range registry.Lifecycles() {
		if err := l.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Stop(context.Background()) })
	}
	server.Config.Handler = handler
	server.Start()
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &site{t: t, url: server.URL, client: client, sender: sender}
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// submit GETs path's form, then POSTs it back with fields and its CSRF
// token, as a browser would.
func (s *site) submit(path string, fields url.Values) *http.Response {
	s.t.Helper()
	page := s.get(path)
	match := csrfField.FindStringSubmatch(page)
	if match == nil {
		s.t.Fatalf("GET %s has no form:\n%s", path, page)
	}
	form := url.Values{"csrf_token": {match[1]}}
	for k, v := range fields {
		form[k] = v
	}
	response, err := s.client.PostForm(s.url+path, form)
	if err != nil {
		s.t.Fatal(err)
	}
	response.Body.Close()
	return response
}

func (s *site) get(path string) string {
	s.t.Helper()
	response, err := s.client.Get(s.url + path)
	if err != nil {
		s.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return string(body)
}

func (s *site) status(path string) int {
	s.t.Helper()
	response, err := s.client.Get(s.url + path)
	if err != nil {
		s.t.Fatal(err)
	}
	response.Body.Close()
	return response.StatusCode
}

// emailed waits for the nth email and returns its link's path and query.
func (s *site) emailed(n int) (mail.Message, string) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(s.sender.Messages()) < n {
		if time.Now().After(deadline) {
			s.t.Fatalf("got %d emails, want %d", len(s.sender.Messages()), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	message := s.sender.Messages()[n-1]
	link := regexp.MustCompile(`https?://\S+`).FindString(message.Text)
	parsed, err := url.Parse(link)
	if err != nil || link == "" {
		s.t.Fatalf("email has no link:\n%s", message.Text)
	}
	return message, parsed.RequestURI()
}

// TestRegisterVerifyAndResetAPassword walks the README: register, confirm
// the emailed address, reach the verified-only page, reset the password,
// and log in with the new one.
func TestRegisterVerifyAndResetAPassword(t *testing.T) {
	s := newSite(t)

	if r := s.submit("/accounts/register/", url.Values{"email": {"ada@example.com"}, "password": {"first-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("register = %d", r.StatusCode)
	}
	if got := s.status("/"); got != http.StatusForbidden {
		t.Fatalf("home before verifying = %d, want 403", got)
	}

	verification, verifyLink := s.emailed(1)
	if verification.To != "ada@example.com" || !strings.HasPrefix(verifyLink, "/accounts/verify/") {
		t.Fatalf("verification email to %q with link %q", verification.To, verifyLink)
	}
	if r := s.submit(verifyLink, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("confirming = %d", r.StatusCode)
	}
	if got := s.status("/"); got != http.StatusOK {
		t.Fatalf("home after verifying = %d, want 200", got)
	}

	if r := s.submit("/accounts/password-reset/", url.Values{"email": {"ada@example.com"}}); r.StatusCode != http.StatusOK {
		t.Fatalf("asking for a reset = %d", r.StatusCode)
	}
	_, resetLink := s.emailed(2)
	if !strings.HasPrefix(resetLink, "/accounts/password-reset/confirm/") {
		t.Fatalf("reset link = %q", resetLink)
	}
	if r := s.submit(resetLink, url.Values{"password": {"second-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("setting the new password = %d", r.StatusCode)
	}
	if got := s.status("/"); got != http.StatusFound {
		t.Fatalf("home after the reset = %d, want a redirect: the reset ends every session", got)
	}
	if r := s.submit("/accounts/login/", url.Values{"email": {"ada@example.com"}, "password": {"second-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("logging in with the new password = %d", r.StatusCode)
	}
	if got := s.status("/"); got != http.StatusOK {
		t.Fatalf("home after logging in again = %d, want 200", got)
	}
}
