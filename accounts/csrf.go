package accounts

import (
	"crypto/subtle"
	"net/http"

	"github.com/angvp/tango"
)

// csrfFieldName is the hidden form field every accounts form carries its
// CSRF token in.
const csrfFieldName = "csrf_token"

// preSessionCSRFCookieName is a short-lived cookie CSRF-protecting the
// pre-session forms (login, register) — there is no AccountSession yet at
// that point to derive a token from, unlike a form rendered after login.
// Scoped to /accounts/ so both forms share one cookie.
const preSessionCSRFCookieName = "tango_account_pre_session_csrf"

// ensurePreSessionCSRFCookie returns the current pre-session CSRF cookie's
// value, issuing a fresh one first if none exists yet, to be rendered as a
// pre-session form's hidden csrf_token field.
func ensurePreSessionCSRFCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(preSessionCSRFCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     preSessionCSRFCookieName,
		Value:    token,
		Path:     "/accounts/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

// verifyPreSessionCSRF reports whether the submitted csrf_token form value
// matches the pre-session CSRF cookie set when the form was rendered.
func verifyPreSessionCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(preSessionCSRFCookieName)
	if err != nil {
		return false
	}
	submitted := r.PostFormValue(csrfFieldName)
	return submitted != "" && subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) == 1
}

// forbiddenCSRF writes a generic 403 for a rejected CSRF token.
func forbiddenCSRF(ctx *tango.Context) error {
	return ctx.JSON(http.StatusForbidden, map[string]string{"error": "invalid or missing CSRF token"})
}
