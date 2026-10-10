package admin

import (
	"net/url"
	"strings"
)

// safeHref reports whether raw may be rendered as a changelist link. It
// accepts a relative URL (path-relative or root-relative) or an absolute
// http or https URL. Anything else is refused, including scheme-relative
// "//host" and "\\host" forms, userinfo, control or whitespace characters,
// and every other scheme (javascript:, data:, mailto:, ...). Refused values
// are rendered as plain text by the caller.
func safeHref(raw string) (string, bool) {
	if raw == "" || strings.ContainsAny(raw, `\`) || hasControlOrSpace(raw) {
		return "", false
	}
	if strings.HasPrefix(raw, "//") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme == "" {
		return raw, parsed.Host == "" && parsed.Opaque == "" && parsed.User == nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return "", false
	}
	return raw, true
}

// hasControlOrSpace reports whether s contains any ASCII control character,
// space, or DEL. Browsers strip some of these from URLs, so they are refused
// outright rather than normalized.
func hasControlOrSpace(s string) bool {
	for _, r := range s {
		if r <= ' ' || r == 0x7f {
			return true
		}
	}
	return false
}
