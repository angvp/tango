package accounts_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
)

const resetRequest = "/accounts/api/password-reset/"
const resetConfirm = "/accounts/api/password-reset/confirm/"

func emailBody(email string) string { return `{"email":"` + email + `"}` }

func confirmBody(token, password string) string {
	return `{"token":"` + token + `","password":"` + password + `"}`
}

// resetTokenFor asks for a reset through the JSON endpoint and returns the
// emailed token.
func resetTokenFor(t *testing.T, site *mailSite, email string) string {
	t.Helper()
	before := len(site.sender.Messages())
	if got := site.postJSON(resetRequest, emailBody(email)).Code; got != http.StatusAccepted {
		t.Fatalf("reset request status %d", got)
	}
	message := site.sent(before + 1)[before]
	if !strings.Contains(message.Text, "https://app.example.com/reset#token=") || strings.Contains(message.Text, "/accounts/password-reset/") {
		t.Fatalf("the reset mail links the wrong place:\n%s", message.Text)
	}
	return linkToken.FindStringSubmatch(message.Text)[1]
}

func TestResetRequestIsTheSame202ForEveryAddressAndMailsOnlyAnActiveAccount(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	site.createAccount("bob@example.com", false)

	var bodies []string
	for _, email := range []string{"Alice@Example.com", "nobody@example.com", "bob@example.com"} {
		response := site.postJSON(resetRequest, emailBody(email))
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s: status %d: %s", email, response.Code, response.Body.String())
		}
		assertJSONHeaders(t, response)
		bodies = append(bodies, response.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("response bodies differ:\n%s\n%s", bodies[0], b)
		}
	}
	messages := site.messages()
	if len(messages) != 1 || messages[0].To != "alice@example.com" || !strings.Contains(messages[0].Text, "https://app.example.com/reset#token=") {
		t.Fatalf("messages = %+v, want one reset mail to alice using ResetURL", messages)
	}
}

func TestResetRequestRefusesAMalformedOrOverLongAddress(t *testing.T) {
	for _, email := range []string{"not an address", "", strings.Repeat("é", 250) + "@example.com"} {
		site := newJSONSite(t, &fakeAuth{})
		response := site.postJSON(resetRequest, emailBody(email))
		body := decode(t, response)
		fields, _ := body["fields"].(map[string]any)
		if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || fields["email"] == nil {
			t.Fatalf("%q: status %d: %s", email, response.Code, response.Body.String())
		}
	}
}

func TestResetRequestsShareTheHTMLCooldownAndRateLimit(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	for i := 0; i < 5; i++ {
		site.postJSON(resetRequest, emailBody("alice@example.com"))
	}
	if got := site.postJSON(resetRequest, emailBody("alice@example.com")).Code; got != http.StatusTooManyRequests {
		t.Fatalf("sixth request status %d, want 429", got)
	}
	if messages := site.messages(); len(messages) != 1 {
		t.Fatalf("%d messages for five requests, want 1 (the cooldown)", len(messages))
	}
}

func TestConfirmSetsThePasswordConsumesTheTokenAndEndsSessions(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)
	account := site.createAccount("alice@example.com", true)
	session := site.logIn("alice@example.com", "old-password")
	token := resetTokenFor(t, site, "alice@example.com")

	response := site.postJSON(resetConfirm, confirmBody(token, "brand-new-password"))
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("status %d, body %q; want 204 and no body", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if len(auth.issued) != 0 {
		t.Fatal("confirming a reset issued a grant")
	}
	if got := site.postJSON("/accounts/api/login/", loginBody("alice@example.com", "brand-new-password")).Code; got != http.StatusOK {
		t.Fatalf("login with the new password: %d", got)
	}
	if got := site.postJSON("/accounts/api/login/", loginBody("alice@example.com", "old-password")).Code; got != http.StatusUnauthorized {
		t.Fatalf("login with the old password: %d", got)
	}
	if rows := site.tokens(); len(rows) != 0 {
		t.Fatalf("tokens left: %+v", rows)
	}
	if site.account(account.ID).EmailVerifiedAt.IsZero() {
		t.Fatal("a completed reset should verify the email, as the HTML flow does")
	}
	if got := site.get("/private/", session).Code; got != http.StatusFound {
		t.Fatalf("an old cookie session still works after the reset: %d", got)
	}
	// Bearer tokens are stateless and non-revocable: the reset never claims
	// to have ended the ones already issued.
	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	bearer(account)(request)
	if id, ok, _ := auth.Authenticate(request.Context(), request); !ok || id != account.ID {
		t.Fatal("a previously issued bearer token stopped working: the reset must not claim revocation")
	}
}

func TestEveryUnusableResetTokenIsTheSameInvalidToken(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	used := resetTokenFor(t, site, "alice@example.com")
	site.postJSON(resetConfirm, confirmBody(used, "brand-new-password"))
	site.advance(10 * time.Minute)
	expired := resetTokenFor(t, site, "alice@example.com")
	site.advance(2 * time.Hour)

	var bodies []string
	for _, token := range []string{used, expired, "no-such-token", ""} {
		response := site.postJSON(resetConfirm, confirmBody(token, "another-password"))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("token %q: status %d", token, response.Code)
		}
		bodies = append(bodies, response.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("invalid-token bodies differ:\n%s\n%s", bodies[0], b)
		}
	}
	if !strings.Contains(bodies[0], `"code":"invalid_token"`) {
		t.Fatalf("body = %s, want code invalid_token", bodies[0])
	}
}

func TestAWeakNewPasswordIs422AndKeepsTheTokenUsable(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	token := resetTokenFor(t, site, "alice@example.com")

	response := site.postJSON(resetConfirm, confirmBody(token, "short"))
	body := decode(t, response)
	fields, _ := body["fields"].(map[string]any)
	if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || fields["password"] == nil {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if got := site.postJSON(resetConfirm, confirmBody(token, "a-good-password")).Code; got != http.StatusNoContent {
		t.Fatalf("the token was used up by a refused password: status %d", got)
	}
}

func TestResetTokenWorksOnlyForTheAddressItWasSentTo(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	account := site.createAccount("alice@example.com", true)
	token := resetTokenFor(t, site, "alice@example.com")
	site.updateAccount(account.ID, func(a *accounts.Account) { a.Email = "other@example.com" })

	if got := site.postJSON(resetConfirm, confirmBody(token, "a-good-password")).Code; got != http.StatusBadRequest {
		t.Fatalf("a token for the old address: status %d, want 400", got)
	}
}
