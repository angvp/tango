// Package locale is the example's language switcher: one POST view that
// remembers the visitor's choice in a cookie. The admin never sets it; the
// host decides how a language is chosen and stored.
package locale

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// CookieName is the cookie that remembers the chosen language, and Param the
// query parameter that overrides it for one request.
const (
	CookieName = "lang"
	Param      = "lang"
)

// Resolver is the order in which the app finds a request's language: the
// query parameter first, then the cookie, then Accept-Language.
func Resolver() i18n.Resolver {
	return i18n.Chain(i18n.FromQuery(Param), i18n.FromCookie(CookieName))
}

// New constructs the "locale" app with POST /language/.
func New() tango.App {
	return tango.NewApp("locale", func(registry *tango.Registry) error {
		return registry.Routes().Include("/language/", tango.URLs{
			tango.Path(http.MethodPost, "/", choose, tango.Name("choose")),
		})
	})
}

// choose stores a registered language in the cookie and goes back to a path
// on this site. A language with no catalog, or a "next" that leaves the site,
// is ignored.
func choose(ctx *tango.Context) error {
	r := ctx.Request()
	if err := r.ParseForm(); err != nil {
		return err
	}
	if lang := registered(r.PostForm.Get(Param)); lang != "" {
		http.SetCookie(ctx.ResponseWriter(), &http.Cookie{
			Name:     CookieName,
			Value:    lang,
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   r.TLS != nil,
		})
	}
	return ctx.Redirect(localPath(r.PostForm.Get("next")))
}

// registered returns the registered spelling of lang, or "" when no catalog
// answers to it. It asks the same resolver the app uses to read the value.
func registered(lang string) string {
	probe := &http.Request{URL: &url.URL{RawQuery: url.Values{Param: {lang}}.Encode()}, Header: http.Header{}}
	return i18n.FromQuery(Param).Resolve(probe)
}

// localPath returns next when it is a path on this site, else "/".
func localPath(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return "/"
	}
	return next
}
