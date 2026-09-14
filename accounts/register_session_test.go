package accounts_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/angvp/tango/accounts"
)

func TestRegisterSuccessSetsSessionCookieAndRedirectsToDefault(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"frank@example.com"}, "password": {"correct-password"}})

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect)", response.Code, http.StatusFound)
	}
	if got := response.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want %q (default post-login destination)", got, "/")
	}

	var found bool
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" && cookie.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("no session cookie set on successful registration")
	}
}

func TestRegisterSuccessRedirectsToSafeNext(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"grace@example.com"}, "password": {"correct-password"}, "next": {"/dashboard/"}})

	if got := response.Header().Get("Location"); got != "/dashboard/" {
		t.Fatalf("Location = %q, want %q", got, "/dashboard/")
	}
}

func TestRegisterSuccessIgnoresUnsafeNext(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"heidi@example.com"}, "password": {"correct-password"}, "next": {"https://evil.example.com/"}})

	if got := response.Header().Get("Location"); got != "/" {
		t.Fatalf("Location = %q, want %q (unsafe next ignored)", got, "/")
	}
}

func TestRegisterFailureDoesNotCreateSessionOrCookie(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t)

	response := postRegister(t, handler, url.Values{"email": {"ivan@example.com"}, "password": {"short"}})

	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			t.Fatal("session cookie set despite registration failure")
		}
	}
}

func TestRegisterSessionDurationIsConfigurable(t *testing.T) {
	handler, store := buildRegisterTestHandler(t, accounts.WithSessionDuration(time.Hour))

	response := postRegister(t, handler, url.Values{"email": {"judy@example.com"}, "password": {"correct-password"}})

	var token string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "tango_account_session" {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie set")
	}

	var row struct{ ExpiresAt time.Time }
	if err := store.QueryRow(t.Context(), &row, "SELECT expires_at AS ExpiresAt FROM account_session WHERE token = ?", token); err != nil {
		t.Fatalf("query session: %v", err)
	}

	wantAround := time.Now().UTC().Add(time.Hour)
	if diff := row.ExpiresAt.Sub(wantAround); diff > time.Minute || diff < -time.Minute {
		t.Fatalf("ExpiresAt = %v, want approximately %v (1h from now)", row.ExpiresAt, wantAround)
	}
}

func TestRegisterSessionCookieNameIsConfigurable(t *testing.T) {
	handler, _ := buildRegisterTestHandler(t, accounts.WithSessionCookieName("custom_session"))

	response := postRegister(t, handler, url.Values{"email": {"kevin@example.com"}, "password": {"correct-password"}})

	var found bool
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "custom_session" {
			found = true
		}
	}
	if !found {
		t.Fatal("custom session cookie name not used")
	}
}
