package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
)

const verifyPath = "/accounts/verify/?token="

// register registers email through the form and returns the session
// cookie.
func (s *mailSite) register(email string) *http.Cookie {
	s.t.Helper()
	response := s.postForm("/accounts/register/", url.Values{"email": {email}, "password": {"old-password"}}, func(r *http.Request) { r.RemoteAddr = "198.51.100.2:1" })
	if response.Code != http.StatusFound {
		s.t.Fatalf("register = %d: %s", response.Code, response.Body.String())
	}
	for _, c := range response.Result().Cookies() {
		if c.Name == accounts.DefaultSessionCookieName {
			return c
		}
	}
	s.t.Fatal("registration set no session cookie")
	return nil
}

func (s *mailSite) verify(token string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.postForm(verifyPath+url.QueryEscape(token), nil)
}

// resend POSTs the resend action with session, the way the RequireVerified
// page's form does.
func (s *mailSite) resend(session *http.Cookie) *httptest.ResponseRecorder {
	s.t.Helper()
	page := s.get("/verified/", session)
	var csrf *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == "tango_account_pre_session_csrf" {
			csrf = c
		}
	}
	form := url.Values{}
	if csrf != nil {
		form.Set("csrf_token", csrf.Value)
	}
	request := httptest.NewRequest(http.MethodPost, "/accounts/verify/resend/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrf != nil {
		request.AddCookie(csrf)
	}
	if session != nil {
		request.AddCookie(session)
	}
	response := httptest.NewRecorder()
	s.handler.ServeHTTP(response, request)
	return response
}

func (s *mailSite) accountByEmail(email string) accounts.Account {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	var rows []accounts.Account
	if err := s.store.List(t0(), meta, dbWhereEmail(email), &rows); err != nil || len(rows) != 1 {
		s.t.Fatalf("account %q: %v (%d rows)", email, err, len(rows))
	}
	return rows[0]
}

func TestRegisteringEmailsAVerificationLink(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	session := site.register("alice@example.com")
	if site.get("/private/", session).Code != http.StatusOK {
		t.Fatal("an unverified account isn't logged in after registering")
	}
	message := site.sent(1)[0]
	if message.To != "alice@example.com" || !strings.Contains(strings.ToLower(message.Subject), "confirm") {
		t.Fatalf("message to %q, subject %q", message.To, message.Subject)
	}
	tokenIn(t, message, testBaseURL+"/accounts/verify/")
	rows := site.tokens()
	if len(rows) != 1 || rows[0].Purpose != accounts.PurposeEmailVerification || !rows[0].ExpiresAt.Equal(site.clock().Add(24*time.Hour)) {
		t.Fatalf("token rows = %+v, want one 24-hour verification token", rows)
	}
}

func TestAVerificationLinkVerifiesOnlyOnPost(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.register("alice@example.com")
	token := tokenIn(t, site.sent(1)[0], testBaseURL+"/accounts/verify/")

	for i := 0; i < 2; i++ {
		response := site.get(verifyPath + token)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<button") {
			t.Fatalf("GET = %d %q", response.Code, response.Body.String())
		}
		tokenPageHeaders(t, response)
	}
	if !site.accountByEmail("alice@example.com").EmailVerifiedAt.IsZero() {
		t.Fatal("a GET verified the account")
	}
	response := site.verify(token)
	if response.Code != http.StatusOK {
		t.Fatalf("POST = %d %q", response.Code, response.Body.String())
	}
	tokenPageHeaders(t, response)
	if got := site.accountByEmail("alice@example.com").EmailVerifiedAt; !got.Equal(site.clock()) {
		t.Fatalf("EmailVerifiedAt = %s, want %s", got, site.clock())
	}
	if response := site.verify(token); response.Code != http.StatusBadRequest {
		t.Fatalf("a second POST = %d, want the invalid-link page", response.Code)
	}
}

func TestOnlyAVerificationTokenForTheCurrentAddressVerifies(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	bob := site.createAccount("bob@example.com", true)
	site.insertToken(alice, "reset-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.insertToken(bob, "moved-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))
	site.updateAccount(bob.ID, func(a *accounts.Account) { a.Email = "bob@new.example.com" })
	for _, token := range []string{"reset-token", "moved-token"} {
		if response := site.verify(token); response.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want the invalid-link page", token, response.Code)
		}
	}
	if !site.account(alice.ID).EmailVerifiedAt.IsZero() || !site.account(bob.ID).EmailVerifiedAt.IsZero() {
		t.Fatal("an unusable token verified an account")
	}
}

func TestResendIssuesANewLinkWithinTheCooldown(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	session := site.register("alice@example.com")
	first := tokenIn(t, site.sent(1)[0], testBaseURL+"/accounts/verify/")

	if response := site.resend(session); response.Code != http.StatusOK {
		t.Fatalf("resend in the cooldown = %d", response.Code)
	}
	site.advance(5*time.Minute + time.Second)
	if response := site.resend(session); response.Code != http.StatusOK {
		t.Fatalf("resend = %d %q", response.Code, response.Body.String())
	}
	site.stop()
	second := tokenIn(t, site.sent(2)[1], testBaseURL+"/accounts/verify/")
	if second == first {
		t.Fatal("resend sent the same token")
	}
	site.start()
	if response := site.verify(first); response.Code != http.StatusBadRequest {
		t.Fatalf("the replaced link = %d, want the invalid-link page", response.Code)
	}
	if response := site.verify(second); response.Code != http.StatusOK {
		t.Fatalf("the new link = %d", response.Code)
	}
}

func TestResendNeedsALoginAndACSRFToken(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	session := site.register("alice@example.com")
	site.advance(5*time.Minute + time.Second)

	loggedOut := site.resend(nil)
	if loggedOut.Code != http.StatusFound || !strings.HasPrefix(loggedOut.Header().Get("Location"), "/accounts/login/") {
		t.Fatalf("logged-out resend = %d to %q, want a redirect to login", loggedOut.Code, loggedOut.Header().Get("Location"))
	}
	request := httptest.NewRequest(http.MethodPost, "/accounts/verify/resend/", nil)
	request.AddCookie(session)
	noCSRF := httptest.NewRecorder()
	site.handler.ServeHTTP(noCSRF, request)
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("resend without a CSRF token = %d, want 403", noCSRF.Code)
	}
	site.stop()
	if got := len(site.sender.Messages()); got != 1 {
		t.Fatalf("sent %d, want only the registration email", got)
	}
}

func TestRequireVerifiedGuardsAViewButNotLogin(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)

	loggedOut := site.get("/verified/")
	if loggedOut.Code != http.StatusFound || !strings.HasPrefix(loggedOut.Header().Get("Location"), "/accounts/login/") {
		t.Fatalf("logged out = %d to %q, want a redirect to login", loggedOut.Code, loggedOut.Header().Get("Location"))
	}

	session := site.register("alice@example.com")
	unverified := site.get("/verified/", session)
	if unverified.Code != http.StatusForbidden || !strings.Contains(unverified.Header().Get("Content-Type"), "text/html") || !strings.Contains(unverified.Body.String(), `action="/accounts/verify/resend/"`) {
		t.Fatalf("unverified = %d %q %q, want a 403 HTML page with a resend action", unverified.Code, unverified.Header().Get("Content-Type"), unverified.Body.String())
	}
	if site.logIn("alice@example.com", "old-password") == nil {
		t.Fatal("an unverified account can't log in")
	}

	site.verify(tokenIn(t, site.sent(1)[0], testBaseURL+"/accounts/verify/"))
	if verified := site.get("/verified/", session); verified.Code != http.StatusOK {
		t.Fatalf("verified = %d", verified.Code)
	}
}

func TestWithoutMailNoVerificationRouteExists(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)
	for _, target := range []string{"/accounts/verify/", "/accounts/verify/resend/", "/accounts/password-reset/confirm/"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(method, target, nil))
			if response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = %d, want it unmounted", method, target, response.Code)
			}
			if response.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s = 405: the path is mounted", method, target)
			}
		}
	}
}

func TestResendIsRateLimitedPerClient(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	session := site.register("alice@example.com")
	var last *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		last = site.resend(session)
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth resend = %d, want 429 with Retry-After", last.Code)
	}
}
