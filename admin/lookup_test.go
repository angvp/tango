package admin_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// lookupPost is a model whose foreign key to fkAuthor is declared as a lookup.
type lookupAnswer struct {
	Items []struct {
		Value string `json:"value"`
		Text  string `json:"text"`
	} `json:"items"`
	More bool `json:"more"`
}

// buildLookupAdmin wires fkAuthor and fkPost with the given options and
// returns the error from installing the admin, so a test can assert a
// startup failure.
func buildLookupAdmin(t *testing.T, authorOptions *admin.Options, postOptions admin.Options) (http.Handler, *sql.DB, error) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(fkAuthor{}); err != nil {
		t.Fatalf("register fkAuthor: %v", err)
	}
	if authorOptions != nil {
		if err := registry.Admin().Register(fkAuthor{}, *authorOptions); err != nil {
			t.Fatalf("register admin fkAuthor: %v", err)
		}
	}
	if err := registry.Models().Register(fkPost{}); err != nil {
		t.Fatalf("register fkPost: %v", err)
	}
	if err := registry.Admin().Register(fkPost{}, postOptions); err != nil {
		t.Fatalf("register admin fkPost: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, nil, err
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB, nil
}

func lookupPostOptions(lookup admin.Widget) admin.Options {
	return admin.Options{Widgets: map[string]admin.Widget{"AuthorID": lookup}}
}

func mustBuildLookupAdmin(t *testing.T, authorOptions admin.Options, lookup admin.Widget) (http.Handler, *sql.DB) {
	t.Helper()
	handler, sqlDB, err := buildLookupAdmin(t, &authorOptions, lookupPostOptions(lookup))
	if err != nil {
		t.Fatalf("install admin: %v", err)
	}
	return handler, sqlDB
}

func decodeLookup(t *testing.T, response *httptest.ResponseRecorder) lookupAnswer {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", response.Code, response.Body.String())
	}
	var answer lookupAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
	return answer
}

const lookupPath = "/admin/fk_post/_lookup/AuthorID/"

func TestLookupSearchesTheSearchFieldsAndReturnsValueAndLabel(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	seedAuthor(t, sqlDB, "Jane Doe")
	seedAuthor(t, sqlDB, "John Roe")
	seedAuthor(t, sqlDB, "Janet Poe")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Jan", nil))
	if len(answer.Items) != 2 || answer.Items[0].Text != "Jane Doe" || answer.Items[1].Text != "Janet Poe" {
		t.Fatalf("items = %+v, want Jane Doe and Janet Poe in primary key order", answer.Items)
	}
	if answer.Items[0].Value != "1" || answer.More {
		t.Fatalf("first value = %q more = %v, want \"1\" and false", answer.Items[0].Value, answer.More)
	}
}

func TestLookupUsesTheFieldsOwnSearchWhenGiven(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name"}, admin.Lookup(admin.LookupOptions{Search: []string{"Name"}}))
	seedAuthor(t, sqlDB, "Jane Doe")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Doe", nil))
	if len(answer.Items) != 1 || answer.Items[0].Text != "Jane Doe" {
		t.Fatalf("items = %+v, want Jane Doe", answer.Items)
	}
}

func TestLookupEmptySearchReturnsTheFirstPageAndSaysWhenThereIsMore(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	for i := 1; i <= 11; i++ {
		seedAuthor(t, sqlDB, fmt.Sprintf("Author %02d", i))
	}

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath, nil))
	if len(answer.Items) != 10 || !answer.More {
		t.Fatalf("got %d items, more = %v; want 10 and true", len(answer.Items), answer.More)
	}
	exact := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Author+1", nil))
	if len(exact.Items) != 2 || exact.More {
		t.Fatalf("q=Author 1: got %d items, more = %v; want 2 and false", len(exact.Items), exact.More)
	}
}

func TestLookupMatchesAnIntegerPrimaryKeyExactly(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	for i := 1; i <= 12; i++ {
		seedAuthor(t, sqlDB, fmt.Sprintf("Writer %d", i))
	}

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=7", nil))
	if len(answer.Items) != 1 || answer.Items[0].Value != "7" {
		t.Fatalf("items = %+v, want only primary key 7 (Writer 7 also matches by name)", answer.Items)
	}
}

func TestLookupFallsBackToThePrimaryKeyWithoutALabel(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	seedAuthor(t, sqlDB, "Jane Doe")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Jane", nil))
	if len(answer.Items) != 1 || answer.Items[0].Text != "1" {
		t.Fatalf("items = %+v, want the primary key as text", answer.Items)
	}
}

func TestLookupCarriesNoStoreAndOnlyValueAndText(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	seedAuthor(t, sqlDB, "Jane Doe")

	response := doRequest(t, handler, http.MethodGet, lookupPath, nil)
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if body := response.Body.String(); strings.Contains(body, "ID") || strings.Contains(body, "Name") {
		t.Fatalf("body %s exposes a column name; want only value and text", body)
	}
}

func TestLookupRequiresAStaffSession(t *testing.T) {
	handler, _ := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))

	request := httptest.NewRequest(http.MethodGet, lookupPath+"?q=a", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther && response.Code != http.StatusFound {
		t.Fatalf("anonymous status = %d, want a redirect to the login", response.Code)
	}
	if location := response.Header().Get("Location"); !strings.HasPrefix(location, "/admin/login/") {
		t.Fatalf("anonymous Location = %q, want the admin login", location)
	}
}

func TestLookupRouteExistsOnlyForLookupFieldsAndOnlyForGet(t *testing.T) {
	handler, _ := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))

	if response := doRequest(t, handler, http.MethodGet, "/admin/fk_post/_lookup/Title/", nil); response.Code != http.StatusNotFound {
		t.Fatalf("a field that is not a lookup: status = %d, want 404", response.Code)
	}
	if response := doRequest(t, handler, http.MethodPost, lookupPath, nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", response.Code)
	}
}

func TestLookupFailsAtStartupWhenItCannotWork(t *testing.T) {
	label := &admin.Options{Label: "Name"}
	cases := []struct {
		name      string
		author    *admin.Options
		post      admin.Options
		mentioned string
	}{
		{"no search fields", label, lookupPostOptions(admin.Lookup(admin.LookupOptions{})), "fkPost.AuthorID"},
		{"search field is not a string", label, lookupPostOptions(admin.Lookup(admin.LookupOptions{Search: []string{"ID"}})), "ID"},
		{"search field is unknown", label, lookupPostOptions(admin.Lookup(admin.LookupOptions{Search: []string{"Nope"}})), "Nope"},
		{"target is not admin-registered", nil, lookupPostOptions(admin.Lookup(admin.LookupOptions{Search: []string{"Name"}})), "not registered with the admin"},
		{"field is not a foreign key", label, admin.Options{Widgets: map[string]admin.Widget{"Title": admin.Lookup(admin.LookupOptions{Search: []string{"Name"}})}}, "fkPost.Title"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := buildLookupAdmin(t, tc.author, tc.post)
			if !errors.Is(err, admin.ErrInvalidLookup) {
				t.Fatalf("err = %v, want ErrInvalidLookup", err)
			}
			if !strings.Contains(err.Error(), tc.mentioned) {
				t.Fatalf("err = %q, want it to mention %q", err, tc.mentioned)
			}
		})
	}
}

func TestLookupAnswers500WhenTheSearchFails(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	// The posts reference the authors, so they go first on every dialect.
	for _, drop := range []string{`DROP TABLE fk_post`, `DROP TABLE fk_author`} {
		if _, err := sqlDB.Exec(drop); err != nil {
			t.Fatalf("%s: %v", drop, err)
		}
	}

	response := doRequest(t, handler, http.MethodGet, lookupPath+"?q=a", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if strings.Contains(response.Body.String(), "fk_author") {
		t.Fatalf("body %q leaks the database error", response.Body.String())
	}
}
