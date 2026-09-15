package admin

import (
	"net/url"

	"github.com/angvp/tango/internal/security"
)

const (
	adminPreselectFieldParam = "_tango_admin_preselect_field"
	adminPreselectValueParam = "_tango_admin_preselect_value"
)

func safeAdminNext(raw string, fallback string) string {
	return security.SafeRedirect(raw, "/admin/", fallback)
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
