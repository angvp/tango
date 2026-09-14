package accounts

import (
	"context"
	"net/http"
	"time"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
)

// defaultSessionDuration is deliberately much longer than admin's fixed
// 24-hour session — a public user of an ordinary web app commonly expects
// to stay signed in across visits in a way an admin operator does not.
// See ADR 0021.
const defaultSessionDuration = 30 * 24 * time.Hour

// defaultSessionCookieName is the session cookie's default name, override
// via WithSessionCookieName.
const defaultSessionCookieName = "tango_account_session"

// defaultPostLoginRedirect is where a login or registration redirects when
// no safe next value is present.
const defaultPostLoginRedirect = "/"

// createAccountSession creates an AccountSession for accountID using
// auth.CreateSession, and sets the session cookie on the response.
func createAccountSession(ctx context.Context, store *db.Store, cfg accountsConfig, w http.ResponseWriter, r *http.Request, accountID int64) error {
	_, sessionMeta := accountModelMetas()
	token, expiresAt, err := auth.CreateSession(ctx, store, sessionMeta, accountID, cfg.sessionDuration)
	if err != nil {
		return err
	}
	setSessionCookie(w, r, cfg.sessionCookieName, token, expiresAt)
	return nil
}

// setSessionCookie sets the session cookie. Secure is only set when the
// request itself arrived over TLS, mirroring admin's own precedent — see
// docs/limitations.md. Path is "/" (not "/accounts/"): a host's own views
// anywhere on the site use the current-account helper, which needs the
// cookie sent on every request, not just ones under /accounts/.
func setSessionCookie(w http.ResponseWriter, r *http.Request, name string, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie removes the session cookie, e.g. on logout.
func clearSessionCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}
