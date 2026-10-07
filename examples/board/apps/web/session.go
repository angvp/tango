package web

import (
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
)

const loginPath = "/accounts/login/"

// requireLogin sends visitors without a valid session to the login page,
// with ?next= pointing back at the page they wanted.
func (p *pages) requireLogin(view tango.View) tango.View {
	return accounts.RequireLogin(p.store, accounts.DefaultSessionCookieName, loginPath, view)
}

// sessionToken is the visitor's session cookie value, or "" without one.
func sessionToken(ctx *tango.Context) string {
	cookie, err := ctx.Request().Cookie(accounts.DefaultSessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// csrfToken is derived from the session token, so it's unique per session
// and can only be produced by someone who holds the session cookie.
func csrfToken(ctx *tango.Context) string {
	if token := sessionToken(ctx); token != "" {
		return auth.DeriveCSRFToken(token)
	}
	return ""
}

// validCSRF checks the csrf_token field of a submitted form.
func validCSRF(ctx *tango.Context) bool {
	token := sessionToken(ctx)
	return token != "" && auth.VerifyCSRFToken(ctx.Request().PostFormValue("csrf_token"), token)
}

// displayName shows the part of an email before the @, so pages never
// publish anyone's full address.
func displayName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	return name
}
