package main

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

// acceptLanguage sends every request with an Accept-Language header, as a
// browser set to that language does.
type acceptLanguage struct{ language string }

func (a acceptLanguage) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Accept-Language", a.language)
	return http.DefaultTransport.RoundTrip(r)
}

func newSiteIn(t *testing.T, language string) *site {
	t.Helper()
	s := newSite(t)
	s.client.Transport = acceptLanguage{language}
	return s
}

// TestSpanishRegisterVerifyAndReset runs the README's steps in Spanish and
// reads the emails: subject, body and a link that works.
func TestSpanishRegisterVerifyAndReset(t *testing.T) {
	i18ntest.Isolate(t)
	var mu sync.Mutex
	var missing []string
	i18n.OnMissing(func(locale, key string) {
		mu.Lock()
		defer mu.Unlock()
		missing = append(missing, locale+" "+key)
	})
	s := newSiteIn(t, "es-MX, es;q=0.8")

	form := s.get("/accounts/register/")
	for _, want := range []string{`<html lang="es">`, "Crear una cuenta", "Registrarse"} {
		if !strings.Contains(form, want) {
			t.Errorf("register page lacks %q", want)
		}
	}
	if r := s.submit("/accounts/register/", url.Values{"email": {"ada@example.com"}, "password": {"short"}}); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("a short password = %d", r.StatusCode)
	}
	if r := s.submit("/accounts/register/", url.Values{"email": {"ada@example.com"}, "password": {"first-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("register = %d", r.StatusCode)
	}

	verification, verifyLink := s.emailed(1)
	if verification.Subject != "Confirma tu dirección de correo" || !strings.Contains(verification.Text, "Confirma que esta es tu dirección") {
		t.Fatalf("verification email is not Spanish: %+v", verification)
	}
	if page := s.get(verifyLink); !strings.Contains(page, "Confirmar mi correo") {
		t.Errorf("confirmation page is not Spanish")
	}
	if r := s.submit(verifyLink, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("confirming = %d", r.StatusCode)
	}
	if got := s.status("/"); got != http.StatusOK {
		t.Fatalf("home after verifying = %d", got)
	}

	if r := s.submit("/accounts/password-reset/", url.Values{"email": {"ada@example.com"}}); r.StatusCode != http.StatusOK {
		t.Fatalf("asking for a reset = %d", r.StatusCode)
	}
	reset, resetLink := s.emailed(2)
	if reset.Subject != "Restablece tu contraseña" || !strings.Contains(reset.Text, "abre este enlace en la próxima hora") {
		t.Fatalf("reset email is not Spanish: %+v", reset)
	}
	if r := s.submit(resetLink, url.Values{"password": {"second-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("the emailed link did not work: %d", r.StatusCode)
	}
	if r := s.submit("/accounts/login/", url.Values{"email": {"ada@example.com"}, "password": {"nope-nope-nope"}}); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong password = %d", r.StatusCode)
	}
	if r := s.submit("/accounts/login/", url.Values{"email": {"ada@example.com"}, "password": {"second-password"}}); r.StatusCode != http.StatusFound {
		t.Fatalf("logging in with the new password = %d", r.StatusCode)
	}

	// The error paths the happy path does not reach.
	s.submit("/accounts/register/", url.Values{"email": {"a@example.com"}})
	s.submit("/accounts/register/", url.Values{"email": {"a@example.com"}, "password": {strings.Repeat("x", 80)}})
	s.submit("/accounts/register/", url.Values{"email": {"ada@example.com"}, "password": {"another-password"}})
	s.submit("/accounts/password-reset/", url.Values{"email": {"not an address"}})
	s.get("/accounts/password-reset/confirm/?token=bad")

	mu.Lock()
	defer mu.Unlock()
	if len(missing) > 0 {
		t.Errorf("keys the accounts app used that the Spanish catalog lacks: %v", missing)
	}
	report := i18n.Validate()
	for _, problem := range report.Problems {
		t.Errorf("catalog problem: %+v", problem)
	}
	// Only what this configuration cannot reach may stay unobserved: signup
	// is open, and the pages for an unverified account and a resent link
	// need a second session.
	for _, unobserved := range report.Unobserved {
		switch unobserved.Key {
		case "accounts.register.closed", "accounts.register.closed_title",
			"accounts.unverified.heading", "accounts.unverified.body", "accounts.unverified.submit",
			"accounts.verification_sent.body":
		default:
			t.Errorf("catalog key the walk never used: %q", unobserved.Key)
		}
	}
}

func TestEnglishRequestsStayEnglishWithABareHTMLTag(t *testing.T) {
	i18ntest.Isolate(t)
	for _, language := range []string{"en", "fr-FR", ""} {
		s := newSiteIn(t, language)
		page := s.get("/accounts/login/")
		if !strings.Contains(page, "<html>\n") || !strings.Contains(page, "<h1>Log in</h1>") || strings.Contains(page, "lang=") {
			t.Errorf("Accept-Language %q: the page is not the plain English one", language)
		}
	}
	s := newSiteIn(t, "en")
	s.submit("/accounts/register/", url.Values{"email": {"ada@example.com"}, "password": {"first-password"}})
	if message, _ := s.emailed(1); message.Subject != "Confirm your email address" {
		t.Errorf("English subject = %q", message.Subject)
	}
}
