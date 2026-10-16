package accounts_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
	"github.com/angvp/tango/openapi"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/openapi_accounts.golden.json")

func jsonOpenAPI(t *testing.T) []byte {
	t.Helper()
	_, store := migratedAccountsDB(t)
	registry := tango.NewRegistry()
	config := accounts.JSONConfig{Auth: &fakeAuth{}, VerifyURL: testVerifyURL, ResetURL: testResetURL}
	mailOpt := accounts.WithMail(accounts.MailConfig{Sender: &mailtest.Sender{}, From: "Shop <noreply@example.com>", BaseURL: testBaseURL})
	for _, app := range []tango.App{accounts.New(store, accounts.WithJSON(config), mailOpt), openapi.New(openapi.Options{Title: "Accounts"})} {
		if err := registry.Register(app); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

func TestAccountsJSONEndpointsAreDocumented(t *testing.T) {
	got := jsonOpenAPI(t)
	const golden = "testdata/openapi_accounts.golden.json"
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the OpenAPI document of the accounts JSON endpoints changed; review it and run go test ./accounts -run TestAccountsJSONEndpointsAreDocumented -update\n%s", got)
	}
}

// The documented session schema must name exactly the members a real login
// returns, so the document cannot drift from the handler.
func TestDocumentedSessionMatchesARealLogin(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(jsonOpenAPI(t), &doc); err != nil {
		t.Fatal(err)
	}
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	body := decode(t, site.postJSON("/accounts/api/login/", `{"identifier":"alice@example.com","password":"old-password"}`))
	compare := func(schema string, actual map[string]any) {
		var documented, real []string
		for name := range doc.Components.Schemas[schema].Properties {
			documented = append(documented, name)
		}
		for name := range actual {
			real = append(real, name)
		}
		sort.Strings(documented)
		sort.Strings(real)
		if len(documented) == 0 || !equalStrings(documented, real) {
			t.Errorf("%s documents %v, a real login returns %v", schema, documented, real)
		}
	}
	compare("Session", body)
	compare("AccountSummary", body["account"].(map[string]any))
	compare("AuthGrant", body["grant"].(map[string]any))
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
