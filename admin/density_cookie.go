package admin

import "net/http"

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
