package admin

import (
	"net/url"
	"strings"

	"github.com/angvp/tango/internal/security"
)

const (
	adminPreselectFieldParam = "_tango_admin_preselect_field"
	adminPreselectValueParam = "_tango_admin_preselect_value"
)

func safeAdminNext(raw string, fallback string) string {
	return security.SafeRedirect(raw, "/admin/", fallback)
}

// withContentFocus points a redirect at the content region. The notice that had
// focus is gone once the page reloads, and the fragment moves focus to the
// region instead of leaving it on the page body.
func withContentFocus(target string) string {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	return target + "#main"
}

func appendQuery(rawURL string, key string, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	values := parsed.Query()
	values.Set(key, value)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}
