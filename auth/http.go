package auth

import (
	"net/http"
	"net/url"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// RequireLogin wraps next and redirects unauthenticated requests to
// loginPath, carrying a safe next query parameter validated against
// allowedPrefix.
func RequireLogin(store *db.Store, sessionMeta model.ModelMeta, cookieName string, loginPath string, allowedPrefix string, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		if _, ok, err := CurrentUserID(ctx, store, sessionMeta, cookieName); err != nil {
			return err
		} else if ok {
			return next(ctx)
		}

		nextTarget := SafeRedirect(ctx.Request().URL.RequestURI(), allowedPrefix, allowedPrefix)
		target := appendNext(loginPath, nextTarget)
		return ctx.Redirect(target)
	}
}

// CurrentUserID returns the current request's session-backed user id. ok is
// false when the cookie is missing, invalid, or expired.
func CurrentUserID(ctx *tango.Context, store *db.Store, sessionMeta model.ModelMeta, cookieName string) (any, bool, error) {
	cookie, err := ctx.Request().Cookie(cookieName)
	if err != nil {
		if err == http.ErrNoCookie {
			return nil, false, nil
		}
		return nil, false, err
	}
	return SessionUser(ctx.Context(), store, sessionMeta, cookie.Value)
}

func appendNext(loginPath string, nextTarget string) string {
	parsed, err := url.Parse(loginPath)
	if err != nil {
		return loginPath
	}
	values := parsed.Query()
	values.Set("next", nextTarget)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}
