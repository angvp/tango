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

const confirmPath = "/accounts/password-reset/confirm/?token="

func (s *mailSite) setPassword(token, password string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.postForm(confirmPath+url.QueryEscape(token), url.Values{"password": {password}})
}

func TestTheResetLinkShowsAFormWithoutUsingTheToken(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	token := site.resetToken("alice@example.com")

	for i := 0; i < 3; i++ { // a mail scanner, a preview, then the person
		response := site.get(confirmPath + token)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `name="password"`) {
			t.Fatalf("GET %d = %d %q", i, response.Code, response.Body.String())
		}
		tokenPageHeaders(t, response)
	}
	if response := site.setPassword(token, "new-password"); response.Code != http.StatusFound {
		t.Fatalf("POST after GETs = %d, want the token still usable", response.Code)
	}
}

func TestResettingThePasswordEndsEveryLogin(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	account := site.createAccount("alice@example.com", true)
	session := site.logIn("alice@example.com", "old-password")
	if session == nil || site.get("/private/", session).Code != http.StatusOK {
		t.Fatal("couldn't log in before the reset")
	}
	replaced := site.resetToken("alice@example.com")
	site.advance(5*time.Minute + time.Second)
	token := site.resetToken("alice@example.com") // a newer link replaces the first
	if response := site.setPassword(replaced, "new-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("the replaced link = %d, want the invalid-link page", response.Code)
	}

	response := site.setPassword(token, "new-password")
	if response.Code != http.StatusFound || response.Header().Get("Location") != "/accounts/login/" {
		t.Fatalf("POST = %d to %q, want a redirect to login", response.Code, response.Header().Get("Location"))
	}
	tokenPageHeaders(t, response)
	if site.get("/private/", session).Code == http.StatusOK {
		t.Fatal("a session from before the reset still works")
	}
	if site.logIn("alice@example.com", "old-password") != nil {
		t.Fatal("the old password still logs in")
	}
	if site.logIn("alice@example.com", "new-password") == nil {
		t.Fatal("the new password doesn't log in")
	}
	if site.account(account.ID).EmailVerifiedAt.IsZero() {
		t.Fatal("a completed reset didn't verify the email")
	}
	if rows := site.tokens(); len(rows) != 0 {
		t.Fatalf("tokens left after the reset: %+v", rows)
	}
	if response := site.setPassword(token, "another-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("reusing the token = %d, want the invalid-link page", response.Code)
	}
}

func TestEveryUnusableResetLinkGetsTheSamePage(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	bob := site.createAccount("bob@example.com", true)
	carol := site.createAccount("carol@example.com", true)
	dave := site.createAccount("dave@example.com", true)

	site.insertToken(alice, "expired-token", accounts.PurposePasswordReset, site.clock().Add(-time.Second))
	site.insertToken(alice, "verification-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))
	site.insertToken(bob, "inactive-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.updateAccount(bob.ID, func(a *accounts.Account) { a.Active = false })
	site.insertToken(carol, "moved-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.updateAccount(carol.ID, func(a *accounts.Account) { a.Email = "carol@new.example.com" })
	site.insertToken(dave, "used-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	if response := site.setPassword("used-token", "new-password"); response.Code != http.StatusFound {
		t.Fatalf("using dave's token = %d", response.Code)
	}

	tokens := []string{"unknown-token", "expired-token", "verification-token", "inactive-token", "moved-token", "used-token", ""}
	var page string
	for _, token := range tokens {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := site.get(confirmPath + token)
			if method == http.MethodPost {
				response = site.setPassword(token, "new-password")
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s %q = %d, want 400", method, token, response.Code)
			}
			tokenPageHeaders(t, response)
			if page == "" {
				page = response.Body.String()
			}
			if response.Body.String() != page {
				t.Fatalf("%s %q answered differently:\n%s\nvs\n%s", method, token, response.Body.String(), page)
			}
		}
	}
	if site.logIn("carol@new.example.com", "new-password") != nil || site.logIn("bob@example.com", "new-password") != nil {
		t.Fatal("an unusable link changed a password")
	}
}

func TestAWeakNewPasswordIsRefusedAndTheLinkStillWorks(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	token := site.resetToken("alice@example.com")
	response := site.setPassword(token, "short")
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Password must be at least 8 characters.") {
		t.Fatalf("weak password = %d %q", response.Code, response.Body.String())
	}
	tokenPageHeaders(t, response)
	if response := site.setPassword(token, "long-enough"); response.Code != http.StatusFound {
		t.Fatalf("after a refused password the link = %d, want it still usable", response.Code)
	}
}
