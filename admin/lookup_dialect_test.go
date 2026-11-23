package admin_test

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

// These tests run on every dialect, so a difference in how the databases
// compare text shows up as a failing run and not as a surprise in production.

func TestLookupCaseSensitivityFollowsTheDialect(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t, admin.Options{Label: "Name", Search: []string{"Name"}}, admin.Lookup(admin.LookupOptions{}))
	seedAuthor(t, sqlDB, "Jane Doe")

	exact := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=Jane", nil))
	if len(exact.Items) != 1 {
		t.Fatalf("q=Jane matched %d rows on every dialect, want 1", len(exact.Items))
	}

	// LIKE ignores ASCII case on SQLite only. PostgreSQL compares exactly, and
	// the tables tango creates on MySQL use utf8mb4_bin, which does too.
	lower := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath+"?q=jane", nil))
	wantMatch := testdb.Dialect() == db.SQLite
	if got := len(lower.Items) == 1; got != wantMatch {
		t.Fatalf("q=jane on dialect %v: matched = %v, want %v", testdb.Dialect(), got, wantMatch)
	}
}

func TestLookupOrdersByTheTargetsOrderingWithTheKeyLast(t *testing.T) {
	handler, sqlDB := mustBuildLookupAdmin(t,
		admin.Options{Label: "Name", Search: []string{"Name"}, Ordering: []string{"-Name"}},
		admin.Lookup(admin.LookupOptions{}))
	first := seedAuthor(t, sqlDB, "Mia")
	second := seedAuthor(t, sqlDB, "Mia")
	third := seedAuthor(t, sqlDB, "Zoe")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, lookupPath, nil))
	want := []int64{third, first, second}
	if len(answer.Items) != len(want) {
		t.Fatalf("got %d items, want %d", len(answer.Items), len(want))
	}
	for i, id := range want {
		if answer.Items[i].Value != itoa(id) {
			t.Fatalf("position %d is %s, want %d (descending name, then key)", i, answer.Items[i].Value, id)
		}
	}
}

type lookupCountry struct {
	Code string `tango:"pk,varchar=8"`
	Name string `tango:"varchar=40"`
}

type lookupCity struct {
	ID          int64  `tango:"pk"`
	Title       string `tango:"varchar=40"`
	CountryCode string `tango:"fk=lookupCountry,varchar=8"`
}

func TestLookupOnAStringKeyMatchesOnlyThroughTheSearchFields(t *testing.T) {
	registry := tango.NewRegistry()
	for _, m := range []any{lookupCountry{}, lookupCity{}} {
		if err := registry.Models().Register(m); err != nil {
			t.Fatalf("register %T: %v", m, err)
		}
	}
	if err := registry.Admin().Register(lookupCountry{}, admin.Options{Label: "Name", Search: []string{"Name"}}); err != nil {
		t.Fatalf("register admin lookupCountry: %v", err)
	}
	if err := registry.Admin().Register(lookupCity{}, admin.Options{Widgets: map[string]admin.Widget{"CountryCode": admin.Lookup(admin.LookupOptions{})}}); err != nil {
		t.Fatalf("register admin lookupCity: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	insertCountry(t, sqlDB, "42", "Alpha")
	insertCountry(t, sqlDB, "AR", "Argentina 42")

	answer := decodeLookup(t, doRequest(t, handler, http.MethodGet, "/admin/lookup_city/_lookup/CountryCode/?q=42", nil))
	if len(answer.Items) != 1 || answer.Items[0].Value != "AR" || answer.Items[0].Text != "Argentina 42" {
		t.Fatalf("items = %+v, want only AR: a key that is not an integer is not matched by key", answer.Items)
	}
}

func insertCountry(t *testing.T, sqlDB *sql.DB, code, name string) {
	t.Helper()
	if _, err := sqlDB.Exec(`INSERT INTO lookup_country (code, name) VALUES ($1, $2)`, code, name); err != nil {
		t.Fatalf("insert country: %v", err)
	}
}
