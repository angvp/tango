package accounts_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
	"github.com/angvp/tango/internal/i18nbridge"
	"github.com/angvp/tango/mail/mailtest"
)

// requireSQLite skips a test whose failure is injected with SQLite trigger
// syntax. The default test database is SQLite; a run pointed at another
// dialect by TANGO_TEST_DSN cannot run these.
func requireSQLite(t *testing.T) {
	t.Helper()
	if dsn := os.Getenv("TANGO_TEST_DSN"); dsn != "" && !strings.HasPrefix(dsn, "sqlite") {
		t.Skip("failure injected with SQLite trigger syntax; TANGO_TEST_DSN names another dialect")
	}
}

// withoutLocaleBridge makes the i18n bridge absent for the test, the state
// a binary reaches only if accounts is built without i18n's init, and
// restores it afterwards.
func withoutLocaleBridge(t *testing.T) {
	t.Helper()
	saved := i18nbridge.Selected
	i18nbridge.Selected = nil
	t.Cleanup(func() { i18nbridge.Selected = saved })
}

// Without the bridge the page cannot name the registered spelling of the
// request's locale, so it declares no language rather than a wrong one. The
// same request with the bridge declares the registered spelling, which is
// the positive control.
func TestWithoutALocaleBridgeThePageDeclaresNoLanguage(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-nobridge", map[string]string{"accounts.login.title": "Entrar"})
	handler, _ := buildRegisterTestHandler(t)
	es := inLocale(handler, "zz-nobridge")
	if body := getPage(es, "/accounts/login/"); !strings.Contains(body, `<html lang="zz-nobridge">`) {
		t.Fatalf("control: page does not declare the registered locale:\n%s", body)
	}
	withoutLocaleBridge(t)
	if body := getPage(es, "/accounts/login/"); strings.Contains(body, "lang=") {
		t.Fatalf("page declares a language without a bridge:\n%s", body)
	}
}

func TestMalformedJSONResetConfirmMembersAreRefused(t *testing.T) {
	tests := []struct {
		name, body string
	}{
		{"body is not an object", `[]`},
		{"token is not a string", `{"token":1,"password":"correct-horse-2"}`},
		{"password is not a string", `{"token":"TOKEN","password":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newJSONSite(t, &fakeAuth{})
			site.createAccount("member@example.com", true)
			token := resetTokenFor(t, site, "member@example.com")
			body := strings.ReplaceAll(tt.body, "TOKEN", token)
			assertJSONError(t, site.postJSON(resetConfirm, body), http.StatusBadRequest)
			// The refused request used nothing up: the same token still works.
			if response := site.postJSON(resetConfirm, `{"token":"`+token+`","password":"correct-horse-2"}`); response.Code != http.StatusNoContent {
				t.Fatalf("valid confirm after the refusal = %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

// blockTokenDeletes installs a trigger on account_token. Deletes are
// skipped (ignore) or fail (abort) from now on, which is how a second
// request consuming the same token between the check and the use looks to
// the code under test.
func blockTokenDeletes(t *testing.T, site *mailSite, name, action string) {
	t.Helper()
	if _, err := site.db.Exec(`CREATE TRIGGER ` + name + ` BEFORE DELETE ON account_token BEGIN SELECT ` + action + `; END`); err != nil {
		t.Fatal(err)
	}
}

func TestAResetTokenConsumedBetweenCheckAndUseIsInvalid(t *testing.T) {
	requireSQLite(t)
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("bob@example.com", true)
	site.createAccount("alice@example.com", true)
	bob := site.resetToken("bob@example.com")
	alice := site.resetToken("alice@example.com")

	// Positive control: with no competing request, the same flow succeeds.
	if response := site.setPassword(bob, "correct-horse-2"); response.Code != http.StatusFound {
		t.Fatalf("control reset = %d: %s", response.Code, response.Body.String())
	}
	blockTokenDeletes(t, site, "lose_race", "RAISE(IGNORE)")

	if response := site.setPassword(alice, "correct-horse-2"); response.Code != http.StatusBadRequest {
		t.Errorf("HTML reset after the token was consumed elsewhere = %d, want the invalid-link page: %s", response.Code, response.Body.String())
	}
}

func TestAResetTokenWhoseDeleteFailsIsAnErrorNotAnInvalidLink(t *testing.T) {
	requireSQLite(t)
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	token := site.resetToken("alice@example.com")
	blockTokenDeletes(t, site, "fail_delete", "RAISE(ABORT, 'blocked')")

	response := site.setPassword(token, "correct-horse-2")
	assertGeneric500(t, "reset whose token delete fails", response.Body.String(), response.Code)
}

func TestVerificationEmailIsSkippedWhenItNoLongerApplies(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, site *mailSite, id int64)
	}{
		{"account deleted", func(t *testing.T, site *mailSite, id int64) {
			meta, _ := accountMetas(t)
			if err := site.store.Delete(context.Background(), meta, id); err != nil {
				t.Fatal(err)
			}
		}},
		{"email already verified", func(t *testing.T, site *mailSite, id int64) {
			site.updateAccount(id, func(a *accounts.Account) { a.EmailVerifiedAt = site.clock() })
		}},
		{"account inactive", func(t *testing.T, site *mailSite, id int64) {
			site.updateAccount(id, func(a *accounts.Account) { a.Active = false })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			site := newMailSite(t, &mailtest.Sender{}, false)
			site.register("gone@example.com")
			site.register("kept@example.com")
			tt.change(t, site, site.accountByEmail("gone@example.com").ID)
			site.start()
			site.stop()

			messages := site.sender.Messages()
			if len(messages) != 1 || messages[0].To != "kept@example.com" {
				t.Fatalf("messages = %+v, want exactly the verification for kept@example.com", messages)
			}
		})
	}
}

func TestVerificationEmailFailsCleanlyWhenTheTokenTableIsGone(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	site.register("alice@example.com")
	dropTable(t, site, "account_token")
	site.start()
	site.stop()

	if messages := site.sender.Messages(); len(messages) != 0 {
		t.Fatalf("messages = %+v, want none", messages)
	}
	if logs := site.logs.byMessage(accounts.EventMailFailed); len(logs) != 1 {
		t.Fatalf("mail_failed logs = %+v, want one", logs)
	}
}

func TestAJSONResetTokenConsumedBetweenCheckAndUseIsInvalid(t *testing.T) {
	requireSQLite(t)
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("bob@example.com", true)
	site.createAccount("alice@example.com", true)
	bob := resetTokenFor(t, site, "bob@example.com")
	alice := resetTokenFor(t, site, "alice@example.com")

	// Positive control: without a competing request the confirm succeeds.
	if response := site.postJSON(resetConfirm, confirmBody(bob, "correct-horse-2")); response.Code != http.StatusNoContent {
		t.Fatalf("control confirm = %d: %s", response.Code, response.Body.String())
	}
	blockTokenDeletes(t, site, "lose_json_race", "RAISE(IGNORE)")

	assertJSONError(t, site.postJSON(resetConfirm, confirmBody(alice, "correct-horse-2")), http.StatusBadRequest)
}

func TestAResetLinkAnswers500WhenTheAccountTableCannotBeRead(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	token := site.resetToken("alice@example.com")
	link := confirmPath + token

	// Positive control: while the account table reads, the link shows its form.
	if response := site.get(link); response.Code != http.StatusOK {
		t.Fatalf("control GET = %d: %s", response.Code, response.Body.String())
	}
	if _, err := site.db.Exec(`ALTER TABLE account RENAME TO account_unreadable`); err != nil {
		t.Fatal(err)
	}

	response := site.get(link)
	assertGeneric500(t, "GET "+link, response.Body.String(), response.Code)
}

func TestVerificationEmailFailsCleanlyWhenTheAccountTableCannotBeRead(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	site.register("alice@example.com")
	if _, err := site.db.Exec(`ALTER TABLE account RENAME TO account_unreadable`); err != nil {
		t.Fatal(err)
	}
	site.start()
	site.stop()

	if messages := site.sender.Messages(); len(messages) != 0 {
		t.Fatalf("messages = %+v, want none", messages)
	}
	if logs := site.logs.byMessage(accounts.EventMailFailed); len(logs) != 1 {
		t.Fatalf("mail_failed logs = %+v, want one", logs)
	}
}
