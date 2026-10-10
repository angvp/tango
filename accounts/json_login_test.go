package accounts_test

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
)

func TestWithJSONFailsTheBuildWhenItsConfigurationIsUnusable(t *testing.T) {
	good := accounts.JSONConfig{Auth: &fakeAuth{}, VerifyURL: testVerifyURL, ResetURL: testResetURL}
	tests := []struct {
		name     string
		mutate   func(*accounts.JSONConfig)
		withMail bool
		want     string
	}{
		{"no Auth", func(c *accounts.JSONConfig) { c.Auth = nil }, true, "Auth"},
		{"no WithMail", func(*accounts.JSONConfig) {}, false, "WithMail"},
		{"no VerifyURL", func(c *accounts.JSONConfig) { c.VerifyURL = "" }, true, "VerifyURL"},
		{"no {token} in ResetURL", func(c *accounts.JSONConfig) { c.ResetURL = "https://app.example.com/reset" }, true, "ResetURL"},
		{"two {token} in VerifyURL", func(c *accounts.JSONConfig) { c.VerifyURL = "https://a.example.com/{token}/{token}" }, true, "VerifyURL"},
		{"relative template", func(c *accounts.JSONConfig) { c.ResetURL = "/reset#token={token}" }, true, "ResetURL"},
		{"userinfo", func(c *accounts.JSONConfig) { c.VerifyURL = "https://user:pw@app.example.com/#{token}" }, true, "VerifyURL"},
		{"not http(s)", func(c *accounts.JSONConfig) { c.VerifyURL = "ftp://app.example.com/#{token}" }, true, "VerifyURL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := good
			tt.mutate(&config)
			err := registrationError(t, config, tt.withMail)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("RunRegistration error = %v, want one naming %q", err, tt.want)
			}
		})
	}
	if err := registrationError(t, good, true); err != nil {
		t.Fatalf("a valid configuration failed: %v", err)
	}
}

func TestWithoutWithJSONNoJSONRouteIsMounted(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	if got := site.postJSON("/accounts/api/login/", `{}`).Code; got != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", got)
	}
}

func TestLoginAnswersWithTheAccountAndAGrantAndTouchesNoCookie(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)
	account := site.createAccount("alice@example.com", true)

	response := site.postJSON("/accounts/api/login/", `{"identifier":"Alice@Example.com","password":"old-password"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("JSON login set cookies: %v", cookies)
	}
	body := decode(t, response)
	acct, _ := body["account"].(map[string]any)
	if acct["id"] != float64(account.ID) || acct["email"] != "alice@example.com" || acct["email_verified"] != false {
		t.Fatalf("account = %v", acct)
	}
	grant, _ := body["grant"].(map[string]any)
	if grant["access_token"] != "tok-"+itoa(account.ID) || grant["token_type"] != "Bearer" || grant["expires_in"] != float64(900) {
		t.Fatalf("grant = %v", grant)
	}
	if len(auth.issued) != 1 || auth.issued[0].PasswordHash != "" {
		t.Fatalf("Issue got %+v, want one account with its password hash withheld", auth.issued)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestEveryLoginFailureIsTheSameGeneric401(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	site.createAccount("gone@example.com", false)

	var bodies []string
	for _, body := range []string{
		`{"identifier":"alice@example.com","password":"wrong-password"}`,
		`{"identifier":"nobody@example.com","password":"old-password"}`,
		`{"identifier":"gone@example.com","password":"old-password"}`,
		`{"identifier":"alice","password":"old-password"}`, // no resolver: not an email
	} {
		response := site.postJSON("/accounts/api/login/", body)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", body, response.Code)
		}
		assertJSONHeaders(t, response)
		bodies = append(bodies, response.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("failure bodies differ:\n%s\n%s", bodies[0], b)
		}
	}
	if !strings.Contains(bodies[0], `"code":"invalid_credentials"`) {
		t.Fatalf("body = %s, want code invalid_credentials", bodies[0])
	}
}

func TestLoginRequestMechanics(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	valid := `{"identifier":"alice@example.com","password":"old-password"}`
	setType := func(v string) func(*http.Request) { return func(r *http.Request) { r.Header.Set("Content-Type", v) } }

	tests := []struct {
		name   string
		body   string
		modify []func(*http.Request)
		status int
		code   string
	}{
		{"a form content type", valid, []func(*http.Request){setType("application/x-www-form-urlencoded")}, 415, "unsupported_media_type"},
		{"no content type", valid, []func(*http.Request){setType("")}, 415, "unsupported_media_type"},
		{"a charset parameter is fine", valid, []func(*http.Request){setType("application/json; charset=utf-8")}, 200, ""},
		{"over 16 KB", `{"identifier":"` + strings.Repeat("a", 17*1024) + `","password":"x"}`, nil, 413, "body_too_large"},
		{"an unknown field", `{"identifier":"a@b.c","password":"x","extra":1}`, nil, 400, "invalid_body"},
		{"a duplicate key", `{"identifier":"a@b.c","identifier":"c@d.e","password":"x"}`, nil, 400, "invalid_body"},
		{"trailing data", valid + ` {}`, nil, 400, "invalid_body"},
		{"a wrong type", `{"identifier":1,"password":"x"}`, nil, 400, "invalid_body"},
		{"null for a required string", `{"identifier":null,"password":"x"}`, nil, 400, "invalid_body"},
		{"an array", `[]`, nil, 400, "invalid_body"},
		{"an empty body", ``, nil, 400, "invalid_body"},
		{"invalid UTF-8", "{\"identifier\":\"a\xff@b.c\",\"password\":\"x\"}", nil, 400, "invalid_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := site.postJSON("/accounts/api/login/", tt.body, tt.modify...)
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.status, response.Body.String())
			}
			assertJSONHeaders(t, response)
			if tt.code != "" && decode(t, response)["code"] != tt.code {
				t.Fatalf("body = %s, want code %s", response.Body.String(), tt.code)
			}
		})
	}
}

func TestLoginAnswersOtherMethodsWith405InTheJSONShape(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		response := site.jsonRequest(method, "/accounts/api/login/", "")
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status %d, want 405", method, response.Code)
		}
		assertJSONHeaders(t, response)
		if decode(t, response)["error"] == nil {
			t.Fatalf("%s: body %s has no error string", method, response.Body.String())
		}
	}
}

func TestAnOverLongIdentifierIsAFieldErrorBeforeAnyLookup(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	long := strings.Repeat("é", 250) + "@example.com" // 262 runes
	response := site.postJSON("/accounts/api/login/", `{"identifier":"`+long+`","password":"x"}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	body := decode(t, response)
	fields, _ := body["fields"].(map[string]any)
	if body["code"] != "invalid_field" || fields["identifier"] == nil {
		t.Fatalf("body = %v, want invalid_field with fields.identifier", body)
	}
	assertJSONHeaders(t, response)
}

func TestLoginFailuresAreRateLimitedWith429(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	for i := 0; i < 5; i++ {
		if got := site.postJSON("/accounts/api/login/", `{"identifier":"x@example.com","password":"wrong"}`).Code; got != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i, got)
		}
	}
	response := site.postJSON("/accounts/api/login/", `{"identifier":"x@example.com","password":"wrong"}`)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q; want 429 with Retry-After", response.Code, response.Header().Get("Retry-After"))
	}
	assertJSONHeaders(t, response)
	if decode(t, response)["code"] != "rate_limited" {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestAFailingIssuerIsAGeneric500WithTheSameHeaders(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)
	site.createAccount("alice@example.com", true)
	auth.setIssueError(errors.New("signing key unavailable"))

	response := site.postJSON("/accounts/api/login/", `{"identifier":"alice@example.com","password":"old-password"}`)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "signing key") {
		t.Fatalf("status %d, body %s; want a generic 500", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
}
