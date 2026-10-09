package accounts_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/mail/mailtest"
)

func TestATokenRowIsUsedUpByTheFirstUseOnly(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	alice := site.createAccount("alice@example.com", true)

	first, second, err := accounts.UseTokenTwice(context.Background(), site.store, alice)
	if err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("uses = %v then %v, want true then false: of two requests holding the same token, one wins", first, second)
	}
}

func TestIssuingATokenReplacesTheEarlierOneOfThatPurposeOnly(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	alice := site.createAccount("alice@example.com", true)
	bob := site.createAccount("bob@example.com", true)
	ctx := context.Background()
	issue := func(account accounts.Account, purpose accounts.TokenPurpose) string {
		t.Helper()
		token, err := accounts.IssueTokenFor(ctx, site.store, account, purpose)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	issue(alice, accounts.PurposePasswordReset)
	issue(alice, accounts.PurposeEmailVerification)
	issue(bob, accounts.PurposePasswordReset)
	if got := len(site.tokens()); got != 3 {
		t.Fatalf("%d tokens, want one per account and purpose", got)
	}
	issue(alice, accounts.PurposePasswordReset)
	rows := site.tokens()
	if len(rows) != 3 {
		t.Fatalf("%d tokens after a replacement, want 3", len(rows))
	}
	reset := 0
	for _, row := range rows {
		if row.AccountID == alice.ID && row.Purpose == accounts.PurposePasswordReset {
			reset++
		}
	}
	if reset != 1 {
		t.Fatalf("alice has %d reset tokens, want 1", reset)
	}
}

func TestIssuingATokenSweepsTheExpiredOnes(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, false)
	alice := site.createAccount("alice@example.com", true)
	bob := site.createAccount("bob@example.com", true)
	site.insertToken(bob, "stale", accounts.PurposePasswordReset, time.Now().Add(-time.Minute))

	if _, err := accounts.IssueTokenFor(context.Background(), site.store, alice, accounts.PurposePasswordReset); err != nil {
		t.Fatal(err)
	}
	for _, row := range site.tokens() {
		if row.AccountID == bob.ID {
			t.Fatalf("bob's expired token survived: %+v", row)
		}
	}
}

func TestATokenLinkAnswersOnlyGetAndPost(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	site.insertToken(alice, "reset-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.insertToken(alice, "verify-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))

	for _, path := range []string{confirmPath + "reset-token", verifyPath + "verify-token"} {
		for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
			response := httptest.NewRecorder()
			site.handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			if response.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, response.Code)
			}
		}
	}
	if got := len(site.tokens()); got != 2 {
		t.Fatalf("%d tokens left, want both untouched", got)
	}
}

func TestAResetLinkDiesWhenItsAccountIsDeleted(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	site.insertToken(alice, "orphan-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	meta, _ := accountMetas(t)
	if err := site.store.Delete(context.Background(), meta, alice.ID); err != nil {
		t.Fatal(err)
	}

	if response := site.get(confirmPath + "orphan-token"); response.Code != http.StatusBadRequest {
		t.Fatalf("GET = %d, want the invalid-link page", response.Code)
	}
	if response := site.setPassword("orphan-token", "new-password"); response.Code != http.StatusBadRequest {
		t.Fatalf("POST = %d, want the invalid-link page", response.Code)
	}
}

func TestADatabaseFailureIsAnErrorNotAnInvalidLink(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	site.insertToken(alice, "reset-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.insertToken(alice, "verify-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))
	if err := site.db.Close(); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{confirmPath + "reset-token", verifyPath + "verify-token"} {
		if response := site.get(path); response.Code != http.StatusInternalServerError {
			t.Errorf("GET %s with the database down = %d, want 500", path, response.Code)
		}
	}
	if _, err := accounts.IssueTokenFor(context.Background(), site.store, alice, accounts.PurposePasswordReset); err == nil {
		t.Error("issuing a token with the database down succeeded")
	}
}

func TestARejectedNewPasswordKeepsTheResetLinkUsable(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	site.insertToken(alice, "reset-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))

	for _, password := range []string{"short", strings.Repeat("x", 73)} {
		response := site.setPassword("reset-token", password)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Password must be") {
			t.Fatalf("POST %d-character password = %d %q, want 400 with the reason", len(password), response.Code, response.Body.String())
		}
		if got := len(site.tokens()); got != 1 {
			t.Fatalf("a rejected password used the token up (%d tokens left)", got)
		}
	}
	if response := site.setPassword("reset-token", "a-good-password"); response.Code != http.StatusFound {
		t.Fatalf("POST with a good password = %d, want a redirect to login", response.Code)
	}
}

func TestConfirmingAnAlreadyVerifiedAddressChangesNothing(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.register("alice@example.com")
	token := tokenIn(t, site.sent(1)[0], testBaseURL+"/accounts/verify/")
	alice := site.accountByEmail("alice@example.com")
	verifiedAt := site.clock().Add(-48 * time.Hour)
	site.updateAccount(alice.ID, func(a *accounts.Account) { a.EmailVerifiedAt = verifiedAt })

	if response := site.verify(token); response.Code != http.StatusOK {
		t.Fatalf("POST = %d, want the confirmation page", response.Code)
	}
	if got := site.accountByEmail("alice@example.com").EmailVerifiedAt; !got.Equal(verifiedAt) {
		t.Fatalf("EmailVerifiedAt = %s, want it left at %s", got, verifiedAt)
	}
}

func TestTheResetRequestPageAnswersOnlyGetAndPost(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	response := httptest.NewRecorder()
	site.handler.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/accounts/password-reset/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT = %d, want 405", response.Code)
	}
}

func TestEveryUnusableVerificationLinkGetsTheSamePage(t *testing.T) {
	site := newMailSite(t, &mailtest.Sender{}, true)
	alice := site.createAccount("alice@example.com", true)
	bob := site.createAccount("bob@example.com", true)
	carol := site.createAccount("carol@example.com", true)

	site.insertToken(alice, "expired-token", accounts.PurposeEmailVerification, site.clock().Add(-time.Second))
	site.insertToken(alice, "reset-token", accounts.PurposePasswordReset, site.clock().Add(time.Hour))
	site.insertToken(bob, "moved-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))
	site.updateAccount(bob.ID, func(a *accounts.Account) { a.Email = "bob@new.example.com" })
	site.insertToken(carol, "used-token", accounts.PurposeEmailVerification, site.clock().Add(time.Hour))
	if response := site.verify("used-token"); response.Code != http.StatusOK {
		t.Fatalf("using carol's token = %d", response.Code)
	}

	var page string
	for _, token := range []string{"unknown-token", "expired-token", "reset-token", "moved-token", "used-token", ""} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := site.get(verifyPath + token)
			if method == http.MethodPost {
				response = site.verify(token)
			}
			if response.Code != http.StatusBadRequest {
				t.Fatalf("%s %q = %d, want 400", method, token, response.Code)
			}
			tokenPageHeaders(t, response)
			if page == "" {
				page = response.Body.String()
			}
			if response.Body.String() != page {
				t.Fatalf("%s %q answered differently:\n%s\nvs\n%s", method, token, response.Body.String(), page)
			}
		}
	}
	for _, email := range []string{"alice@example.com", "bob@new.example.com"} {
		if !site.accountByEmail(email).EmailVerifiedAt.IsZero() {
			t.Fatalf("an unusable link verified %s", email)
		}
	}
}
