package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// hostPreferences is a host model whose JSON field OnRegister writes in the
// account's transaction.
type hostPreferences struct {
	ID        int64  `tango:"pk"`
	AccountID int64  `tango:"fk=Account"`
	Settings  string `tango:"json"`
}

func preferencesMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	for _, m := range []any{accounts.Account{}, hostPreferences{}} {
		if err := registry.Register(m); err != nil {
			t.Fatal(err)
		}
	}
	meta, _ := registry.Get("hostPreferences")
	return meta
}

func withPreferencesTable(t *testing.T, site *mailSite) {
	t.Helper()
	migrationtest.Apply(t, site.db, testdb.Dialect(), []model.ModelMeta{preferencesMeta(t)})
}

func (s *mailSite) preferencesCount() int {
	s.t.Helper()
	n, err := s.store.Count(context.Background(), preferencesMeta(s.t), db.Query{})
	if err != nil {
		s.t.Fatal(err)
	}
	return n
}

// settingsHook writes the profile's "settings" text into the JSON field. When
// extra is set it returns the write's error joined with extra.
func settingsHook(t *testing.T, extra error) func(context.Context, *db.Store, accounts.Registration) error {
	meta := preferencesMeta(t)
	return func(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
		var in struct{ Settings string }
		if err := json.Unmarshal(reg.Profile, &in); err != nil {
			return accounts.FieldError{Field: "profile", Message: "is malformed"}
		}
		err := tx.Create(ctx, meta, &hostPreferences{AccountID: reg.AccountID, Settings: in.Settings})
		if extra != nil {
			return errors.Join(err, extra)
		}
		return err
	}
}

func TestAHookInvalidJSONIs422NamingTheFieldAndRollsBack(t *testing.T) {
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: settingsHook(t, nil)})
	withPreferencesTable(t, site)

	response := registerWithProfile(site, `{"settings":"{secret-oops"}`)
	body := decode(t, response)
	fields, _ := body["fields"].(map[string]any)
	message, _ := fields["Settings"].(string)
	if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || message == "" {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "oops") {
		t.Fatalf("the 422 echoes the submitted document: %s", response.Body.String())
	}
	assertJSONHeaders(t, response)
	if site.accountCount() != 0 || site.preferencesCount() != 0 || len(site.sender.Messages()) != 0 {
		t.Fatal("an invalid JSON value left an account, preferences or mail behind")
	}
}

func TestAHookInvalidJSONJoinedWithAFieldErrorKeepsBothMessages(t *testing.T) {
	extra := accounts.FieldError{Field: "username", Message: "is reserved"}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: settingsHook(t, extra)})
	withPreferencesTable(t, site)

	response := registerWithProfile(site, `{"settings":"{oops"}`)
	fields, _ := decode(t, response)["fields"].(map[string]any)
	if response.Code != http.StatusUnprocessableEntity || fields["Settings"] == nil || fields["username"] != "is reserved" {
		t.Fatalf("status %d: %s; want both fields", response.Code, response.Body.String())
	}
	if site.accountCount() != 0 || site.preferencesCount() != 0 {
		t.Fatal("a joined invalid JSON error left an account or preferences behind")
	}
}
