package accounts

import (
	"context"
	"net/http"
	"time"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// defaultSessionDuration is deliberately much longer than admin's fixed
// 24-hour session — a public user of an ordinary web app commonly expects
// to stay signed in across visits in a way an admin operator does not.
const defaultSessionDuration = 30 * 24 * time.Hour

// DefaultSessionCookieName is the session cookie's default name, override
// via WithSessionCookieName. Exported so a host calling RequireLogin or
// CurrentAccountID/CurrentAccount can reference it instead of
// hardcoding the string when accounts.New was installed with no
// session-cookie-name option.
const DefaultSessionCookieName = "tango_account_session"

// defaultPostLoginRedirect is where a login or registration redirects when
// no safe next value is present.
const defaultPostLoginRedirect = "/"

// createAccountSession creates an AccountSession for accountID using
// auth.CreateSession, and sets the session cookie on the response.
func createAccountSession(ctx context.Context, store *db.Store, cfg accountsConfig, w http.ResponseWriter, r *http.Request, accountID int64) error {
	token, expiresAt, err := auth.CreateSession(ctx, store, sessionMeta(), accountID, cfg.sessionDuration)
	if err != nil {
		return err
	}
	setSessionCookie(w, r, cfg.sessionCookieName, token, expiresAt)
	return nil
}

// setSessionCookie sets the session cookie. Secure is set when the client
// connected over HTTPS, directly or through a TLS-terminating proxy (see
// security.IsHTTPS), mirroring admin's own precedent. Path is "/" (not "/accounts/"): a host's own views
// anywhere on the site use the current-account helper, which needs the
// cookie sent on every request, not just ones under /accounts/.
func setSessionCookie(w http.ResponseWriter, r *http.Request, name string, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
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
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}
