package admin

import (
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// adminRootPath is where a theme switch lands when its next path is unsafe
// or missing.
const adminRootPath = "/admin/"

// themeView handles POST for /admin/theme/, the sidebar's theme switch. It
// runs behind requireSession, so only a staff session reaches it, and it
// checks the session's CSRF token before touching the cookie. Light and dark
// set the cookie, system deletes it, anything else is refused.
func themeView() tango.View {
	return func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		if !verifySessionCSRF(ctx) {
			return forbiddenCSRF(ctx)
		}

		switch theme := ctx.Request().PostForm.Get("theme"); theme {
		case themeLight, themeDark:
			setThemeCookie(ctx.ResponseWriter(), ctx.Request(), theme)
		case themeSystem:
			clearThemeCookie(ctx.ResponseWriter(), ctx.Request())
		default:
			return invalidTheme(ctx)
		}
		return ctx.Redirect(safeAdminThemeNext(ctx.Request().PostForm.Get("next")))
	}
}

func invalidTheme(ctx *tango.Context) error {
	return ctx.JSON(http.StatusBadRequest, map[string]string{"error": i18n.T(ctx.Context(), "admin.error.invalid_theme", "invalid theme")})
}

// safeAdminThemeNext returns raw when it is a same-origin relative path under
// /admin/ after parsing, otherwise adminRootPath.
func safeAdminThemeNext(raw string) string {
	if isSafeAdminPath(raw) {
		return raw
	}
	return adminRootPath
}

// isSafeAdminPath rejects anything that is not a plain relative path under
// /admin/: empty values, backslashes, percent escapes (encoded or
// double-encoded), whitespace and control characters, schemes, hosts,
// protocol-relative forms, and dot segments.
func isSafeAdminPath(raw string) bool {
	if raw == "" || strings.ContainsAny(raw, `\%`) || strings.IndexFunc(raw, isControlOrSpace) >= 0 {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Opaque != "" || parsed.Host != "" || parsed.User != nil {
		return false
	}
	if strings.HasPrefix(raw, "//") || !strings.HasPrefix(parsed.Path, adminRootPath) {
		return false
	}
	return hasNormalizedSegments(parsed.Path)
}

// hasNormalizedSegments reports whether no segment of p is empty, "." or
// "..", ignoring a single trailing slash.
func hasNormalizedSegments(p string) bool {
	segments := strings.Split(strings.TrimSuffix(p, "/"), "/")
	for _, segment := range segments[1:] {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func isControlOrSpace(r rune) bool {
	return unicode.IsControl(r) || unicode.IsSpace(r)
}
