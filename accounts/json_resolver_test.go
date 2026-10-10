package accounts_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
)

// usernames is a host's username directory.
type usernames struct {
	byName map[string]int64
	asked  []string
	err    error
}

func (u *usernames) resolve(_ context.Context, store *db.Store, identifier string) (int64, bool, error) {
	if store == nil {
		return 0, false, errors.New("resolver got no store")
	}
	u.asked = append(u.asked, identifier)
	id, ok := u.byName[identifier]
	return id, ok, u.err
}

func loginBody(identifier, password string) string {
	return `{"identifier":"` + identifier + `","password":"` + password + `"}`
}

func TestAUsernameLogsInThroughTheResolverLikeAnEmail(t *testing.T) {
	names := &usernames{byName: map[string]int64{}}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, ResolveIdentifier: names.resolve})
	account := site.createAccount("alice@example.com", true)
	names.byName["alice"] = account.ID

	response := site.postJSON("/accounts/api/login/", loginBody("alice", "old-password"))
	if response.Code != http.StatusOK {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	acct, _ := decode(t, response)["account"].(map[string]any)
	if acct["id"] != float64(account.ID) || acct["email"] != "alice@example.com" {
		t.Fatalf("account = %v", acct)
	}
	assertJSONHeaders(t, response)
}

func TestAnIdentifierWithAtNeverReachesTheResolver(t *testing.T) {
	names := &usernames{byName: map[string]int64{"a@b": 1}}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, ResolveIdentifier: names.resolve})
	site.createAccount("alice@example.com", true)
	site.postJSON("/accounts/api/login/", loginBody("alice@example.com", "old-password"))
	site.postJSON("/accounts/api/login/", loginBody("a@b", "old-password"))
	if len(names.asked) != 0 {
		t.Fatalf("the resolver was asked %v for identifiers containing @", names.asked)
	}
}

func TestEveryUsernameFailureLooksLikeAWrongPassword(t *testing.T) {
	names := &usernames{byName: map[string]int64{}}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, ResolveIdentifier: names.resolve})
	alice := site.createAccount("alice@example.com", true)
	gone := site.createAccount("gone@example.com", false)
	names.byName["alice"], names.byName["gone"], names.byName["stale"] = alice.ID, gone.ID, 99999

	var bodies []string
	for _, body := range []string{
		loginBody("alice", "wrong-password"),
		loginBody("nobody", "old-password"),
		loginBody("gone", "old-password"),  // inactive
		loginBody("stale", "old-password"), // resolves to a row that no longer exists
	} {
		response := site.postJSON("/accounts/api/login/", body)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d", body, response.Code)
		}
		bodies = append(bodies, response.Body.String())
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Fatalf("bodies differ:\n%s\n%s", bodies[0], b)
		}
	}
}

func TestWithoutAResolverAUsernameFailsLikeAWrongPassword(t *testing.T) {
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	if got := site.postJSON("/accounts/api/login/", loginBody("alice", "old-password")).Code; got != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", got)
	}
}

func TestAResolverErrorIsAGeneric500(t *testing.T) {
	names := &usernames{err: errors.New("directory down: secret-host")}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, ResolveIdentifier: names.resolve})
	response := site.postJSON("/accounts/api/login/", loginBody("alice", "old-password"))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret-host") {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
}
