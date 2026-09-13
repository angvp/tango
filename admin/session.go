package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

// sessionCookieName is the cookie carrying an AdminSession's token.
const sessionCookieName = "tango_admin_session"

// sessionDuration is every session's fixed lifetime from creation — no
// idle timeout, no "remember me" (this milestone's Q6 decision).
const sessionDuration = 24 * time.Hour

// createSession creates a new AdminSession for userID and returns its
// token and expiry.
func createSession(ctx context.Context, store *db.Store, userID int64) (string, time.Time, error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(sessionDuration)

	_, sessionMeta := adminModelMetas()
	session := AdminSession{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}
	if err := store.Create(ctx, sessionMeta, &session); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// sessionUser returns the still-active AdminUser a valid, unexpired
// session token belongs to. ok is false for a missing, expired, or
// deactivated-account session.
func sessionUser(ctx context.Context, store *db.Store, token string) (AdminUser, bool, error) {
	if token == "" {
		return AdminUser{}, false, nil
	}

	userMeta, sessionMeta := adminModelMetas()
	var sessions []AdminSession
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS ID, %s AS Token, %s AS UserID, %s AS ExpiresAt FROM %s WHERE %s = ?",
		db.ColumnName("ID"), db.ColumnName("Token"), db.ColumnName("UserID"), db.ColumnName("ExpiresAt"),
		db.ColumnName(sessionMeta.Name), db.ColumnName("Token"),
	)
	if err := store.Query(ctx, &sessions, sqlQuery, token); err != nil {
		return AdminUser{}, false, err
	}
	if len(sessions) == 0 {
		return AdminUser{}, false, nil
	}
	session := sessions[0]
	if !session.ExpiresAt.After(time.Now().UTC()) {
		return AdminUser{}, false, nil
	}

	var users []AdminUser
	userQuery := fmt.Sprintf(
		"SELECT %s AS ID, %s AS Username, %s AS PasswordHash, %s AS Active, %s AS IsStaff, %s AS IsSuperuser, %s AS CreatedAt FROM %s WHERE %s = ?",
		db.ColumnName("ID"), db.ColumnName("Username"), db.ColumnName("PasswordHash"),
		db.ColumnName("Active"), db.ColumnName("IsStaff"), db.ColumnName("IsSuperuser"),
		db.ColumnName("CreatedAt"), db.ColumnName(userMeta.Name), db.ColumnName("ID"),
	)
	if err := store.Query(ctx, &users, userQuery, session.UserID); err != nil {
		return AdminUser{}, false, err
	}
	if len(users) == 0 || !users[0].Active {
		return AdminUser{}, false, nil
	}
	return users[0], true, nil
}

// deleteSessionByToken removes the AdminSession row for token, if any.
func deleteSessionByToken(ctx context.Context, store *db.Store, token string) error {
	_, sessionMeta := adminModelMetas()
	var sessions []AdminSession
	sqlQuery := fmt.Sprintf(
		"SELECT %s AS ID FROM %s WHERE %s = ?",
		db.ColumnName("ID"), db.ColumnName(sessionMeta.Name), db.ColumnName("Token"),
	)
	if err := store.Query(ctx, &sessions, sqlQuery, token); err != nil {
		return err
	}
	for _, session := range sessions {
		if err := store.Delete(ctx, sessionMeta, session.ID); err != nil {
			return err
		}
	}
	return nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// setSessionCookie sets the session cookie. Secure is only set when the
// request itself arrived over TLS: a fixed Secure=true would silently stop
// login from working over the plain-HTTP localhost most local development
// (and the tutorial) uses, and this framework's documented boundary is
// "TLS if exposed," not "TLS always" — see docs/limitations.md.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// requireSession wraps next so it only runs for a request carrying a valid,
// unexpired session for an active, staff AdminUser. An unauthenticated
// request (missing, invalid, expired, or deactivated-account session)
// redirects to the login page with a "next" query parameter pointing back
// at the original request, per this milestone's Q5 decision. An
// authenticated, active, non-staff account instead gets 403 Forbidden: this
// is a distinct case from "not logged in," and a redirect there would
// wrongly imply signing in again could help. Only IsStaff is checked here;
// IsSuperuser has no effect on this decision.
func requireSession(store *db.Store, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		var token string
		if cookie, err := ctx.Request().Cookie(sessionCookieName); err == nil {
			token = cookie.Value
		}

		user, ok, err := sessionUser(ctx.Context(), store, token)
		if err != nil {
			return err
		}
		if !ok {
			nextPath := safeAdminNext(ctx.Request().URL.Path, "/admin/")
			target := "/admin/login/?next=" + url.QueryEscape(nextPath)
			return ctx.Redirect(target)
		}
		if !user.IsStaff {
			return forbidden(ctx)
		}

		return next(ctx)
	}
}

// forbidden writes a plain 403 Forbidden response for an authenticated,
// active account that lacks staff access.
func forbidden(ctx *tango.Context) error {
	w := ctx.ResponseWriter()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, err := w.Write([]byte("403 Forbidden: this admin account does not have staff access.\n"))
	return err
}
