package admin

import (
	"net/http"

	"github.com/angvp/tango/internal/security"
)

// densityCookieName holds the changelist density, comfortable or compact.
const densityCookieName = "tango_admin_density"

const (
	densityComfortable = "comfortable"
	densityCompact     = "compact"
)

// densityFromRequest returns the changelist density a request asks for. Only
// the exact value compact changes it; a missing or invalid cookie falls back
// to comfortable.
func densityFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(densityCookieName)
	if err == nil && cookie.Value == densityCompact {
		return densityCompact
	}
	return densityComfortable
}

func setDensityCookie(w http.ResponseWriter, r *http.Request, density string) {
	http.SetCookie(w, &http.Cookie{
		Name:     densityCookieName,
		Value:    density,
		Path:     "/admin/",
		MaxAge:   themeCookieMaxAge,
		HttpOnly: true,
		Secure:   security.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}
