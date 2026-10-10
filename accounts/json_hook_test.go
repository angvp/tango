package accounts_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/migrationtest"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/testdb"
)

// hostProfile is a host's own model, written by OnRegister in the account's
// transaction.
type hostProfile struct {
	ID        int64  `tango:"pk"`
	AccountID int64  `tango:"fk=Account"`
	Username  string `tango:"varchar=8,unique"`
	Bio       string
}

func profileMeta(t *testing.T) model.ModelMeta {
	t.Helper()
	registry := model.NewRegistry()
	for _, m := range []any{accounts.Account{}, hostProfile{}} {
		if err := registry.Register(m); err != nil {
			t.Fatal(err)
		}
	}
	meta, _ := registry.Get("hostProfile")
	return meta
}

// withProfileTable creates the host table on the site's database.
func withProfileTable(t *testing.T, site *mailSite) {
	t.Helper()
	migrationtest.Apply(t, site.db, testdb.Dialect(), []model.ModelMeta{profileMeta(t)})
}

func (s *mailSite) profileCount() int {
	s.t.Helper()
	n, err := s.store.Count(context.Background(), profileMeta(s.t), db.Query{})
	if err != nil {
		s.t.Fatal(err)
	}
	return n
}

func (s *mailSite) accountCount() int {
	s.t.Helper()
	meta, _ := accountMetas(s.t)
	n, err := s.store.Count(context.Background(), meta, db.Query{})
	if err != nil {
		s.t.Fatal(err)
	}
	return n
}

// profileHook writes a hostProfile from the request's profile object.
func profileHook(t *testing.T, seen *accounts.Registration) func(context.Context, *db.Store, accounts.Registration) error {
	meta := profileMeta(t)
	return func(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
		if seen != nil {
			*seen = reg
		}
		var in struct{ Username, Bio string }
		if len(reg.Profile) > 0 {
			if err := json.Unmarshal(reg.Profile, &in); err != nil {
				return accounts.FieldError{Field: "profile", Message: "is malformed"}
			}
		}
		return tx.Create(ctx, meta, &hostProfile{AccountID: reg.AccountID, Username: in.Username, Bio: in.Bio})
	}
}

func registerWithProfile(site *mailSite, profile string) *httptest.ResponseRecorder {
	body := `{"email":"New@Example.com","password":"correct-horse"`
	if profile != "" {
		body += `,"profile":` + profile
	}
	return site.postJSON("/accounts/api/register/", body+`}`)
}

func TestOnRegisterWritesTheHostProfileInTheAccountsTransaction(t *testing.T) {
	var seen accounts.Registration
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: profileHook(t, &seen)})
	withProfileTable(t, site)

	response := registerWithProfile(site, `{"username":"alice","bio":"hi"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	account, _ := site.findAccount("new@example.com")
	if seen.AccountID != account.ID || seen.Email != "new@example.com" || !strings.Contains(string(seen.Profile), `"alice"`) {
		t.Fatalf("hook saw %+v for account %d", seen, account.ID)
	}
	var rows []hostProfile
	if err := site.store.List(context.Background(), profileMeta(t), db.Query{}, &rows); err != nil || len(rows) != 1 || rows[0].AccountID != account.ID {
		t.Fatalf("profiles = %+v, %v", rows, err)
	}
	site.sent(1)
}

func TestAHookFieldErrorIs422RollsBackEverythingAndCountsAsAFailedAttempt(t *testing.T) {
	hook := func(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
		_ = tx.Create(ctx, profileMeta(t), &hostProfile{AccountID: reg.AccountID, Username: "x"}) // written, then refused
		return errors.Join(
			accounts.FieldError{Field: "username", Message: "is reserved"},
			accounts.FieldError{Field: "bio", Message: "is too short"},
		)
	}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: hook})
	withProfileTable(t, site)

	response := registerWithProfile(site, `{}`)
	body := decode(t, response)
	fields, _ := body["fields"].(map[string]any)
	if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || fields["username"] != "is reserved" || fields["bio"] != "is too short" {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if site.accountCount() != 0 || site.profileCount() != 0 || len(site.sender.Messages()) != 0 {
		t.Fatalf("accounts %d, profiles %d, mail %d; want nothing left behind", site.accountCount(), site.profileCount(), len(site.sender.Messages()))
	}
	for i := 0; i < 4; i++ {
		registerWithProfile(site, `{}`)
	}
	if got := registerWithProfile(site, `{}`).Code; got != http.StatusTooManyRequests {
		t.Fatalf("after five field errors status = %d, want 429", got)
	}
}

func TestAWrappedJoinOfFieldErrorsKeepsEveryMessage(t *testing.T) {
	hook := func(ctx context.Context, tx *db.Store, reg accounts.Registration) error {
		return fmt.Errorf("profile: %w", errors.Join(
			accounts.FieldError{Field: "username", Message: "is reserved"},
			accounts.FieldError{Field: "bio", Message: "is too short"},
		))
	}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: hook})
	withProfileTable(t, site)

	response := registerWithProfile(site, `{}`)
	fields, _ := decode(t, response)["fields"].(map[string]any)
	if response.Code != http.StatusUnprocessableEntity || fields["username"] != "is reserved" || fields["bio"] != "is too short" {
		t.Fatalf("status %d: %s; want both messages", response.Code, response.Body.String())
	}
}

func TestAHookValueTooLongErrorIs422KeyedByTheGoFieldAndRollsBack(t *testing.T) {
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: profileHook(t, nil)})
	withProfileTable(t, site)

	response := registerWithProfile(site, `{"username":"much-too-long"}`)
	body := decode(t, response)
	fields, _ := body["fields"].(map[string]any)
	message, _ := fields["Username"].(string)
	if response.Code != http.StatusUnprocessableEntity || body["code"] != "invalid_field" || !strings.Contains(message, "8") {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	if site.accountCount() != 0 || site.profileCount() != 0 || len(site.sender.Messages()) != 0 {
		t.Fatal("an over-long hook value left an account, profile or mail behind")
	}
}

func TestAnyOtherHookErrorIsAGeneric500ThatLeaksNothing(t *testing.T) {
	hook := func(context.Context, *db.Store, accounts.Registration) error {
		return errors.New("db password is hunter2")
	}
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: hook})

	response := registerWithProfile(site, ``)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "hunter2") {
		t.Fatalf("status %d: %s", response.Code, response.Body.String())
	}
	assertJSONHeaders(t, response)
	if site.accountCount() != 0 {
		t.Fatal("the account survived a failing hook")
	}
}

func TestAPanickingHookRollsBackTheAccount(t *testing.T) {
	hook := func(context.Context, *db.Store, accounts.Registration) error { panic("host bug") }
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: hook})

	func() {
		defer func() { _ = recover() }()
		registerWithProfile(site, ``)
	}()
	if site.accountCount() != 0 {
		t.Fatal("the account survived a panicking hook")
	}
}

func TestProfileRules(t *testing.T) {
	withHook := newJSONSiteWith
	huge := `{"bio":"` + strings.Repeat("a", 4100) + `"}`
	tests := []struct {
		name    string
		hook    bool
		profile string
		status  int
		code    string
	}{
		{"a profile without a hook", false, `{"username":"a"}`, http.StatusBadRequest, "invalid_body"},
		{"an array", true, `[]`, http.StatusBadRequest, "invalid_body"},
		{"a string", true, `"x"`, http.StatusBadRequest, "invalid_body"},
		{"null", true, `null`, http.StatusBadRequest, "invalid_body"},
		{"over 4 KiB of raw JSON", true, huge, http.StatusRequestEntityTooLarge, "body_too_large"},
		{"an object at the cap's edge is accepted", true, `{"bio":"` + strings.Repeat("a", 4000) + `"}`, http.StatusCreated, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := accounts.JSONConfig{Auth: &fakeAuth{}}
			if tt.hook {
				config.OnRegister = func(context.Context, *db.Store, accounts.Registration) error { return nil }
			}
			site := withHook(t, config)
			response := registerWithProfile(site, tt.profile)
			if response.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", response.Code, tt.status, response.Body.String())
			}
			if tt.code != "" && decode(t, response)["code"] != tt.code {
				t.Fatalf("body = %s, want code %s", response.Body.String(), tt.code)
			}
			assertJSONHeaders(t, response)
		})
	}
}

func TestARegistrationWithoutAProfileGivesTheHookNoProfile(t *testing.T) {
	var seen accounts.Registration
	hook := func(_ context.Context, _ *db.Store, reg accounts.Registration) error { seen = reg; return nil }
	site := newJSONSiteWith(t, accounts.JSONConfig{Auth: &fakeAuth{}, OnRegister: hook})
	if got := registerWithProfile(site, ``).Code; got != http.StatusCreated || len(seen.Profile) != 0 {
		t.Fatalf("status %d, profile %q", got, seen.Profile)
	}
}
