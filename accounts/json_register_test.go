package accounts_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
)

func (s *mailSite) findAccount(email string) (accounts.Account, bool) {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	var rows []accounts.Account
	query := db.Query{Where: []db.Condition{{Field: "Email", Op: db.OpEq, Value: email}}}
	if err := s.store.List(context.Background(), meta, query, &rows); err != nil {
		s.t.Fatal(err)
	}
	if len(rows) == 0 {
		return accounts.Account{}, false
	}
	return rows[0], true
}

func TestRegisterCreatesTheAccountAnswers201AndMailsAJSONLink(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)

	response := site.postJSON("/accounts/api/register/", `{"email":"New@Example.com","password":"correct-horse"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("JSON registration set cookies: %v", cookies)
	}
	body := decode(t, response)
	acct, _ := body["account"].(map[string]any)
	account, ok := site.findAccount("new@example.com")
	if !ok || acct["id"] != float64(account.ID) || acct["email"] != "new@example.com" || acct["email_verified"] != false {
		t.Fatalf("account = %v, stored %+v (%v)", acct, account, ok)
	}
	if grant, _ := body["grant"].(map[string]any); grant["access_token"] != "tok-"+itoa(account.ID) {
		t.Fatalf("grant = %v", grant)
	}

	messages := site.sent(1)
	text := messages[0].Text
	if !strings.Contains(text, "https://app.example.com/verify#token=") || strings.Contains(text, "/accounts/verify/") {
		t.Fatalf("the verification mail links the wrong place:\n%s", text)
	}
	if token := linkToken.FindStringSubmatch(text)[1]; len(site.tokens()) != 1 || site.tokens()[0].TokenHash != sha256Hex(token) {
		t.Fatalf("the emailed token does not match the stored hash")
	}
}

func TestRegisterRefusesADuplicateWith409(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("taken@example.com", true)

	response := site.postJSON("/accounts/api/register/", `{"email":"TAKEN@example.com","password":"correct-horse"}`)
	if response.Code != http.StatusConflict || decode(t, response)["code"] != "already_registered" {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
}

func TestRegisterFieldProblemsAre422WithFields(t *testing.T) {
	tests := []struct {
		name, body, field string
	}{
		{"no email", `{"password":"correct-horse"}`, "email"},
		{"an empty email", `{"email":"  ","password":"correct-horse"}`, "email"},
		{"no password", `{"email":"a@example.com"}`, "password"},
		{"a short password", `{"email":"a@example.com","password":"short"}`, "password"},
		{"a 73 byte password", `{"email":"a@example.com","password":"` + strings.Repeat("a", 73) + `"}`, "password"},
		{"an email over 254 characters", `{"email":"` + strings.Repeat("é", 250) + `@example.com","password":"correct-horse"}`, "email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newJSONSite(t, &fakeAuth{})
			response := site.postJSON("/accounts/api/register/", tt.body)
			body := decode(t, response)
			fields, _ := body["fields"].(map[string]any)
			if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || fields[tt.field] == nil {
				t.Fatalf("status %d: %s; want 422 invalid_field with fields.%s", response.Code, response.Body.String(), tt.field)
			}
			assertJSONHeaders(t, response)
		})
	}
}

func TestRegisterBodyRulesAndMethods(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	if got := site.postJSON("/accounts/api/register/", `{"email":"a@example.com","password":"correct-horse","profile":{}}`).Code; got != http.StatusBadRequest {
		t.Fatalf("an unknown field: status %d, want 400 (profile arrives with the hook)", got)
	}
	if got := site.jsonRequest(http.MethodGet, "/accounts/api/register/", "").Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status %d", got)
	}
}

func TestRegistrationFailuresAreRateLimitedAndTheLimiterIsTheHTMLOne(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("taken@example.com", true)
	for i := 0; i < 5; i++ {
		if got := site.postJSON("/accounts/api/register/", `{"email":"taken@example.com","password":"correct-horse"}`).Code; got != http.StatusConflict {
			t.Fatalf("attempt %d: status %d", i, got)
		}
	}
	if got := site.postJSON("/accounts/api/register/", `{"email":"new@example.com","password":"correct-horse"}`); got.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", got.Code)
	}
	// The HTML form shares the budget for the same client.
	if got := site.postForm("/accounts/register/", map[string][]string{"email": {"x@example.com"}, "password": {"correct-horse"}}); got.Code != http.StatusTooManyRequests {
		t.Fatalf("the HTML registration status %d, want 429 from the shared limiter", got.Code)
	}
}

func TestRegisterWhileSignupIsDisabledIs403(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{}, accounts.WithSignupDisabled())
	response := site.postJSON("/accounts/api/register/", `{"email":"a@example.com","password":"correct-horse"}`)
	if response.Code != http.StatusForbidden || decode(t, response)["code"] != "registration_closed" {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
}

func TestAFailingIssuerLeavesTheRegisteredAccountAndTheClientLogsIn(t *testing.T) {
	auth := &fakeAuth{}
	site := newJSONSite(t, auth)
	auth.setIssueError(context.DeadlineExceeded)

	response := site.postJSON("/accounts/api/register/", `{"email":"new@example.com","password":"correct-horse"}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if _, ok := site.findAccount("new@example.com"); !ok {
		t.Fatal("the account does not exist after an Issue failure")
	}
	auth.setIssueError(nil)
	if got := site.postJSON("/accounts/api/login/", `{"identifier":"new@example.com","password":"correct-horse"}`).Code; got != http.StatusOK {
		t.Fatalf("login after the failure: status %d", got)
	}
	site.sent(1) // the verification mail went out regardless
}
