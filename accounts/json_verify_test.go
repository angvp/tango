package accounts_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
)

const (
	jsonVerifyPath = "/accounts/api/verify/"
	jsonResendPath = "/accounts/api/verify/resend/"
)

func tokenBody(token string) string { return `{"token":"` + token + `"}` }

// registeredViaJSON registers an account through the JSON endpoint and
// returns it with the emailed verification token.
func registeredViaJSON(t *testing.T, site *mailSite, email string) (accounts.Account, string) {
	t.Helper()
	response := site.postJSON("/accounts/api/register/", `{"email":"`+email+`","password":"correct-horse"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", response.Code, response.Body.String())
	}
	message := site.sent(1)[0]
	account, _ := site.findAccount(email)
	return account, linkToken.FindStringSubmatch(message.Text)[1]
}

func TestVerifyMarksTheEmailVerifiedAndConsumesTheToken(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	account, token := registeredViaJSON(t, site, "new@example.com")

	response := site.postJSON(jsonVerifyPath, tokenBody(token))
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("status %d, body %q", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if site.account(account.ID).EmailVerifiedAt.IsZero() {
		t.Fatal("the email is not verified")
	}
	login := decode(t, site.postJSON("/accounts/api/login/", loginBody("new@example.com", "correct-horse")))
	if acct, _ := login["account"].(map[string]any); acct["email_verified"] != true {
		t.Fatalf("account = %v, want email_verified true", acct)
	}
	if got := site.postJSON(jsonVerifyPath, tokenBody(token)).Code; got != http.StatusBadRequest {
		t.Fatalf("a reused token: status %d, want 400", got)
	}
}

func TestEveryUnusableVerificationTokenIsTheSameInvalidToken(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	account, token := registeredViaJSON(t, site, "new@example.com")
	site.updateAccount(account.ID, func(a *accounts.Account) { a.Email = "changed@example.com" })

	var bodies []string
	for _, candidate := range []string{token, "no-such-token", ""} {
		response := site.postJSON(jsonVerifyPath, tokenBody(candidate))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("token %q: status %d", candidate, response.Code)
		}
		bodies = append(bodies, response.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("bodies differ:\n%s\n%s", bodies[0], b)
		}
	}
	if !strings.Contains(bodies[0], `"code":"invalid_token"`) {
		t.Fatalf("body = %s", bodies[0])
	}
}

func TestResendMailsANewLinkToTheAuthenticatedAccount(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	account, _ := registeredViaJSON(t, site, "new@example.com")
	site.advance(6 * time.Minute) // past the per-address cooldown

	response := site.postJSON(jsonResendPath, `{}`, bearer(account))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	messages := site.sent(2)
	if !strings.Contains(messages[1].Text, "https://app.example.com/verify#token=") {
		t.Fatalf("the resent mail links the wrong place:\n%s", messages[1].Text)
	}
}

func TestResendRequiresAValidBearerAndKeepsOperationalFailuresApart(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)
	account, _ := registeredViaJSON(t, site, "new@example.com")

	for name, modify := range map[string]func(*http.Request){
		"no credentials":     func(*http.Request) {},
		"an invalid bearer":  func(r *http.Request) { r.Header.Set("Authorization", "Bearer garbage") },
		"an unknown account": func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok-999999") },
	} {
		response := site.postJSON(jsonResendPath, `{}`, modify)
		if response.Code != http.StatusUnauthorized || decode(t, response)["code"] != "unauthenticated" {
			t.Fatalf("%s: status %d: %s", name, response.Code, response.Body.String())
		}
		assertJSONHeaders(t, response)
	}

	auth.authErr = errors.New("token store down")
	response := site.postJSON(jsonResendPath, `{}`, bearer(account))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "token store") {
		t.Fatalf("status %d: %s; want a generic 500", response.Code, response.Body.String())
	}
}

func TestResendForAVerifiedOrInactiveAccountIsTheSame202WithoutMail(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	verified, token := registeredViaJSON(t, site, "new@example.com")
	site.postJSON(jsonVerifyPath, tokenBody(token))
	inactive := site.createAccount("off@example.com", false)
	site.advance(6 * time.Minute)

	for _, account := range []accounts.Account{verified, inactive} {
		if got := site.postJSON(jsonResendPath, `{}`, bearer(account)).Code; got != http.StatusAccepted {
			t.Fatalf("account %d: status %d, want 202", account.ID, got)
		}
	}
	if messages := site.messages(); len(messages) != 1 {
		t.Fatalf("%d messages, want only the registration mail", len(messages))
	}
}

func TestResendIsRateLimited(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	account, _ := registeredViaJSON(t, site, "new@example.com")
	for i := 0; i < 5; i++ {
		site.postJSON(jsonResendPath, `{}`, bearer(account))
	}
	if got := site.postJSON(jsonResendPath, `{}`, bearer(account)).Code; got != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", got)
	}
}
