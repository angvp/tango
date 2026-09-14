package accounts

import (
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
)

// logoutView handles POST for /accounts/logout/: deletes only the current
// session's row (the account holder's other sessions, if any, are
// untouched) and clears the cookie. There is no GET route for logout.
func logoutView(store *db.Store, cfg accountsConfig) tango.View {
	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodPost {
			return methodNotAllowed(ctx)
		}

		if cookie, err := ctx.Request().Cookie(cfg.sessionCookieName); err == nil {
			_, sessionMeta := accountModelMetas()
			if err := auth.DeleteSession(ctx.Context(), store, sessionMeta, cookie.Value); err != nil {
				return err
			}
		}
		clearSessionCookie(ctx.ResponseWriter(), ctx.Request(), cfg.sessionCookieName)
		return ctx.Redirect("/accounts/login/")
	}
}
