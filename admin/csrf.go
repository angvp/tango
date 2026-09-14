package admin

import (
	"crypto/subtle"
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/internal/security"
)

// csrfFieldName is the hidden form field every admin form (login included)
// carries its CSRF token in.
const csrfFieldName = "csrf_token"

// loginCSRFCookieName is a short-lived, login-page-scoped cookie used only
// to CSRF-protect the login form itself, before any AdminSession exists —
// per this milestone's Q9, session-backed forms instead derive their token
// from the session, needing no separate storage.
const loginCSRFCookieName = "tango_admin_login_csrf"

// sessionCSRFToken derives the CSRF token rendered into an authenticated
// admin form from the current session's token: a hash of it, not the raw
// session token itself, so a form field leaking (e.g. a future XSS bug)
// doesn't directly hand over a hijackable session credential the way
// rendering the session token verbatim would.
func sessionCSRFToken(sessionToken string) string {
	return security.DeriveCSRFToken(sessionToken)
}

// csrfTokenFromRequest returns the CSRF token to render into a form for
// the request's current session, or "" if there is no session cookie
// (which requireSession already guarantees cannot happen for a request
// that reached a protected view).
func csrfTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return sessionCSRFToken(cookie.Value)
}

// verifySessionCSRF reports whether ctx's submitted csrf_token form value
// matches the current session's derived token.
func verifySessionCSRF(ctx *tango.Context) bool {
	cookie, err := ctx.Request().Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	submitted := ctx.Request().PostFormValue(csrfFieldName)
	return security.VerifyCSRFToken(submitted, cookie.Value)
}

// forbiddenCSRF writes a generic 403 for a rejected CSRF token — never a
// silent no-op, per this milestone's ticket 04.
func forbiddenCSRF(ctx *tango.Context) error {
	return ctx.JSON(http.StatusForbidden, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.csrf", "invalid or missing CSRF token")})
}

// ensureLoginCSRFCookie returns the current login-CSRF cookie's value,
// issuing a fresh one first if none exists yet, to be rendered as the
// login form's hidden csrf_token field.
func ensureLoginCSRFCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if cookie, err := r.Cookie(loginCSRFCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     loginCSRFCookieName,
		Value:    token,
		Path:     "/admin/login/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	return token, nil
}

// verifyLoginCSRF reports whether the submitted csrf_token form value
// matches the login-CSRF cookie set when the form was rendered.
func verifyLoginCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(loginCSRFCookieName)
	if err != nil {
		return false
	}
	submitted := r.PostFormValue(csrfFieldName)
	return submitted != "" && subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) == 1
}
