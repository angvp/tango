package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/angvp/tango/internal/security"
)

// themeCookieName is the cookie holding an explicit Light or Dark choice.
// No cookie, or a choice of System, leaves the browser's preference in
// charge.
const themeCookieName = "tango_admin_theme"

// themeCookieMaxAge keeps an explicit choice for one year, in seconds.
const themeCookieMaxAge = 365 * 24 * 60 * 60

const (
	themeLight  = "light"
	themeDark   = "dark"
	themeSystem = "system"
)

// themeState is what a request contributes to its rendered page: the
// explicit theme from its cookie, and the path a theme switch returns to.
type themeState struct {
	Theme string
	Next  string
}

type themeStateKey struct{}

// withThemeState is admin middleware that records the request's theme on
// its context, so the layout can render data-theme and the switch without
// every page passing the request along.
func withThemeState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := themeState{Theme: themeFromRequest(r), Next: r.URL.EscapedPath()}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), themeStateKey{}, state)))
	})
}

func themeStateFrom(ctx context.Context) themeState {
	state, _ := ctx.Value(themeStateKey{}).(themeState)
	return state
}

// Theme is the explicit theme the page renders in, "light" or "dark", or
// "" when no valid choice is stored and the browser's preference decides.
func (c chrome) Theme() string {
	return c.themeState().Theme
}

// ThemeNext is the path the sidebar's theme switch returns to after a
// choice is saved.
func (c chrome) ThemeNext() string {
	return c.themeState().Next
}

func (c chrome) themeState() themeState {
	if c.ctx == nil {
		return themeState{}
	}
	return themeStateFrom(c.ctx)
}

// themeFromRequest returns the explicit theme a request carries, or "" when
// the cookie is missing or holds anything other than light or dark.
func themeFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(themeCookieName)
	if err != nil {
		return ""
	}
	switch cookie.Value {
	case themeLight, themeDark:
		return cookie.Value
	default:
		return ""
	}
}

func setThemeCookie(w http.ResponseWriter, r *http.Request, theme string) {
	http.SetCookie(w, &http.Cookie{
		Name:     themeCookieName,
		Value:    theme,
		Path:     "/admin/",
		MaxAge:   themeCookieMaxAge,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearThemeCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     themeCookieName,
		Value:    "",
		Path:     "/admin/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}
