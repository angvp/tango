package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail"
	"github.com/angvp/tango/mail/mailtest"
)

func TestAskingForAResetEmailsALinkToAnActiveAccount(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	account := site.createAccount("alice@example.com", true)

	if response := site.askForReset("  Alice@Example.com "); response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	message := site.sent(1)[0]
	if message.To != "alice@example.com" || message.From != "Shop <noreply@example.com>" {
		t.Fatalf("message from %q to %q", message.From, message.To)
	}
	if !strings.Contains(strings.ToLower(message.Subject), "password") {
		t.Fatalf("subject = %q", message.Subject)
	}
	token := tokenIn(t, message, testBaseURL+"/accounts/password-reset/confirm/")

	rows := site.tokens()
	if len(rows) != 1 {
		t.Fatalf("token rows = %+v, want one", rows)
	}
	row := rows[0]
	if row.AccountID != account.ID || row.Purpose != accounts.PurposePasswordReset {
		t.Fatalf("token row = %+v", row)
	}
	if row.TokenHash != sha256Hex(token) || row.AddressHash != sha256Hex("alice@example.com") {
		t.Fatalf("token row stores %q / %q, want the SHA-256 of the token and of the address", row.TokenHash, row.AddressHash)
	}
	if want := site.clock().Add(time.Hour); !row.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %s, want %s", row.ExpiresAt, want)
	}
}

func TestResetResponsesNeverRevealAnAccount(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.createAccount("bob@example.com", false)

	tests := []struct{ name, email string }{
		{"existing", "alice@example.com"},
		{"unknown", "nobody@example.com"},
		{"inactive", "bob@example.com"},
		{"in its cooldown", "alice@example.com"},
		{"in its cooldown, spelled differently", "  ALICE@example.com "},
		{"unknown, with a display name", "Nobody <nobody@example.com>"},
	}
	var first *httptest.ResponseRecorder
	for i, tt := range tests {
		// A different client each time, so the per-IP limit stays out of it.
		response := site.askForReset(tt.email, func(r *http.Request) { r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1) })
		if first == nil {
			first = response
			continue
		}
		if response.Code != first.Code || response.Body.String() != first.Body.String() || fmt.Sprint(response.Header()) != fmt.Sprint(first.Header()) {
			t.Fatalf("%s: response %d %v %q differs from the existing account's %d %v %q", tt.name, response.Code, response.Header(), response.Body.String(), first.Code, first.Header(), first.Body.String())
		}
	}
	site.stop()
	site.sent(1)
}

func TestTheResetLinkIgnoresTheRequestsHost(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.askForReset("alice@example.com", func(r *http.Request) {
		r.Host = "evil.example"
		r.Header.Set("X-Forwarded-Host", "evil.example")
	})
	message := site.sent(1)[0]
	if strings.Contains(message.Text, "evil.example") {
		t.Fatalf("the link uses the request's host:\n%s", message.Text)
	}
	tokenIn(t, message, testBaseURL+"/accounts/password-reset/confirm/")
}

func TestAnAddressGetsOneResetEmailPerCooldown(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.askForReset("alice@example.com")
	first := tokenIn(t, site.sent(1)[0], testBaseURL+"/accounts/password-reset/confirm/")

	site.advance(4 * time.Minute)
	site.askForReset("alice@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.9:1" })
	site.advance(time.Minute + time.Second)
	site.askForReset("alice@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.10:1" })
	second := tokenIn(t, site.sent(2)[1], testBaseURL+"/accounts/password-reset/confirm/")

	rows := site.tokens()
	if len(rows) != 1 || rows[0].TokenHash != sha256Hex(second) || second == first {
		t.Fatalf("token rows = %+v; want only the second token", rows)
	}
}

func TestAFullOutboxDropsMailWithoutChangingTheResponse(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false, accounts.WithOutboxCapacity(1))
	site.createAccount("alice@example.com", true)
	site.createAccount("carol@example.com", true)
	queued := site.askForReset("alice@example.com")
	dropped := site.askForReset("carol@example.com")
	if dropped.Code != queued.Code || dropped.Body.String() != queued.Body.String() {
		t.Fatalf("a dropped message changed the response: %d vs %d", dropped.Code, queued.Code)
	}
	logs := site.logs.byMessage(accounts.EventMailDropped)
	if len(logs) != 1 || len(logs[0].attrs) != 1 || logs[0].attrs["purpose"] != "password_reset" {
		t.Fatalf("mail_dropped logs = %+v, want one with only purpose", logs)
	}
	site.start()
	site.stop()
	if got := site.sent(1)[0].To; got != "alice@example.com" {
		t.Fatalf("sent to %q, want the queued message", got)
	}
}

// echoingSender fails with an error that repeats the recipient and text,
// as a careless relay might.
type echoingSender struct{}

func (echoingSender) Send(_ context.Context, m mail.Message) error {
	return fmt.Errorf("550 rejected %s: %s", m.To, m.Text)
}

func TestAFailedSendIsLoggedWithoutTheAddressOrToken(t *testing.T) {
	site := newMailSite(t, echoingSender{}, true)
	site.createAccount("alice@example.com", true)
	site.askForReset("alice@example.com")
	site.stop()
	logs := site.logs.byMessage(accounts.EventMailFailed)
	if len(logs) != 1 || logs[0].attrs["purpose"] != "password_reset" || logs[0].attrs["error"] == "" {
		t.Fatalf("mail_failed logs = %+v", logs)
	}
	for key, value := range logs[0].attrs {
		if key != "purpose" && key != "error" {
			t.Fatalf("mail_failed logs %q", key)
		}
		if strings.Contains(strings.ToLower(value), "alice") || strings.Contains(value, "token=") || strings.Contains(value, "example.com/accounts") {
			t.Fatalf("mail_failed %s = %q leaks the address or the link", key, value)
		}
	}
}

func TestShutdownSendsWhatIsQueued(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	site.createAccount("alice@example.com", true)
	site.createAccount("carol@example.com", true)
	site.askForReset("alice@example.com")
	site.askForReset("carol@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.7:1" })
	site.start()
	site.stop()
	if got := len(site.sender.Messages()); got != 2 {
		t.Fatalf("sent %d before Stop returned, want 2", got)
	}
}

func TestResetRequestsAreRateLimitedPerClient(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = site.askForReset(fmt.Sprintf("user%d@example.com", i))
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth request = %d, want 429 with Retry-After", last.Code)
	}
}

func TestTheResetFormRendersWithACSRFToken(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	response := httptest.NewRecorder()
	site.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/accounts/password-reset/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `name="csrf_token"`) || !strings.Contains(response.Body.String(), `name="email"`) {
		t.Fatalf("GET = %d %q", response.Code, response.Body.String())
	}
}

func TestInvalidMailConfigFailsRegistration(t *testing.T) {
	valid := accounts.MailConfig{Sender: &mailtest.Sender{}, From: "noreply@example.com", BaseURL: "https://example.com"}
	tests := []struct {
		name   string
		change func(*accounts.MailConfig)
		ok     bool
	}{
		{"valid", func(*accounts.MailConfig) {}, true},
		{"localhost over HTTP", func(c *accounts.MailConfig) { c.BaseURL = "http://localhost:8000" }, true},
		{"loopback IP over HTTP", func(c *accounts.MailConfig) { c.BaseURL = "http://127.0.0.1:8000" }, true},
		{"trailing slash", func(c *accounts.MailConfig) { c.BaseURL = "https://example.com/" }, true},
		{"no Sender", func(c *accounts.MailConfig) { c.Sender = nil }, false},
		{"empty From", func(c *accounts.MailConfig) { c.From = "" }, false},
		{"malformed From", func(c *accounts.MailConfig) { c.From = "not an address" }, false},
		{"From with a line break", func(c *accounts.MailConfig) { c.From = "noreply@example.com\r\nBcc: x@example.com" }, false},
		{"empty BaseURL", func(c *accounts.MailConfig) { c.BaseURL = "" }, false},
		{"plain HTTP", func(c *accounts.MailConfig) { c.BaseURL = "http://example.com" }, false},
		{"relative", func(c *accounts.MailConfig) { c.BaseURL = "/accounts" }, false},
		{"other scheme", func(c *accounts.MailConfig) { c.BaseURL = "ftp://example.com" }, false},
		{"no host", func(c *accounts.MailConfig) { c.BaseURL = "https://" }, false},
		{"a path", func(c *accounts.MailConfig) { c.BaseURL = "https://example.com/shop" }, false},
		{"a query", func(c *accounts.MailConfig) { c.BaseURL = "https://example.com?x=1" }, false},
		{"credentials", func(c *accounts.MailConfig) { c.BaseURL = "https://user:pass@example.com" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := valid
			tt.change(&config)
			registry := tango.NewRegistry()
			if err := registry.Register(accounts.New(newTestStore(t), accounts.WithMail(config))); err != nil {
				t.Fatal(err)
			}
			err := registry.RunRegistration()
			if tt.ok && err != nil {
				t.Fatalf("RunRegistration = %v, want nil", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("RunRegistration accepted it")
			}
		})
	}
}

func TestWithoutMailNoResetRouteOrOutboxExists(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Register(accounts.New(newTestStore(t))); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, _ := registry.Routes().Handler()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, "/accounts/password-reset/", nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", method, response.Code)
		}
	}
	if len(registry.Lifecycles()) != 0 {
		t.Fatalf("lifecycles = %+v, want none without mail", registry.Lifecycles())
	}
}

func TestAnInvalidEmailIsAFormError(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	for i, email := range []string{"", "   ", "not an address", "a@b@c", "alice@example.com, bob@example.com"} {
		response := site.askForReset(email, func(r *http.Request) { r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1", i+1) })
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%q = %d, want a 400 form error", email, response.Code)
		}
	}
	site.stop()
	if got := len(site.sender.Messages()); got != 0 {
		t.Fatalf("sent %d for invalid input", got)
	}
}

func TestADroppedEmailGivesBackItsCooldown(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false, accounts.WithOutboxCapacity(1))
	site.createAccount("alice@example.com", true)
	site.createAccount("carol@example.com", true)
	site.askForReset("alice@example.com")
	site.askForReset("carol@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.20:1" }) // dropped: the outbox is full
	site.start()
	site.sent(1)
	site.askForReset("carol@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.21:1" })
	if got := site.sent(2)[1].To; got != "carol@example.com" {
		t.Fatalf("second email went to %q, want carol, whose dropped email shouldn't hold a cooldown", got)
	}
}

// blockingSender blocks every Send until ctx is done, counting sends in
// progress.
type blockingSender struct {
	mu              sync.Mutex
	active, started int
}

func (s *blockingSender) Send(ctx context.Context, _ mail.Message) error {
	s.mu.Lock()
	s.active++
	s.started++
	s.mu.Unlock()
	<-ctx.Done()
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return ctx.Err()
}

func TestShutdownEndsAnInFlightSendAtItsDeadline(t *testing.T) {
	sender := &blockingSender{}
	site := newMailSite(t, sender, true)
	site.createAccount("alice@example.com", true)
	site.createAccount("carol@example.com", true)
	site.askForReset("alice@example.com")
	site.askForReset("carol@example.com", func(r *http.Request) { r.RemoteAddr = "192.0.2.30:1" })
	deadline := time.Now().Add(5 * time.Second)
	for {
		sender.mu.Lock()
		started := sender.started
		sender.mu.Unlock()
		if started == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	var stopErr error
	for _, l := range site.lifecycles {
		stopErr = l.Stop(ctx)
	}
	site.lifecycles = nil
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Stop took %s, want it bounded by its 200ms deadline", elapsed)
	}
	if !errors.Is(stopErr, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want the deadline's error: carol's email was lost", stopErr)
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	if sender.active != 0 || sender.started != 1 {
		t.Fatalf("after Stop: %d sends in progress, %d started; want 0 and 1", sender.active, sender.started)
	}
}

func TestAFailedPreparationLogsOnlyAnErrorClass(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	if err := site.db.Close(); err != nil {
		t.Fatal(err)
	}
	site.askForReset("alice@example.com")
	site.stop()
	logs := site.logs.byMessage(accounts.EventMailFailed)
	if len(logs) != 1 {
		t.Fatalf("mail_failed logs = %+v, want one", logs)
	}
	got := logs[0].attrs["error"]
	if got == "" || strings.Contains(got, "alice") || strings.Contains(strings.ToLower(got), "sql") || strings.Contains(got, "closed") {
		t.Fatalf("mail_failed error = %q, want only a class of error", got)
	}
}

func TestAResetLinkDiesWhenTheEmailChanges(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	account := site.createAccount("alice@example.com", true)
	token := site.resetToken("alice@example.com")
	site.updateAccount(account.ID, func(a *accounts.Account) { a.Email = "alice@new.example.com" })
	if response := site.get(confirmPath + token); response.Code != http.StatusBadRequest {
		t.Fatalf("GET after the email changed = %d, want the invalid-link page", response.Code)
	}
	if response := site.setPassword(token, "new-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("POST after the email changed = %d, want the invalid-link page", response.Code)
	}
}
