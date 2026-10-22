package i18n

import (
	"bufio"
	"net"
	"net/http"
	"strings"

	"github.com/angvp/tango"
)

// Resolver decides the locale of one request. Unlike a bare LocaleResolver it
// also says which request headers its answer depends on, so the middleware
// can tell caches (see Vary).
type Resolver interface {
	// Resolve returns the locale for r, or "" when it has none. The helpers
	// in this package return only a locale that has a registered catalog.
	Resolve(r *http.Request) string
	// Vary returns the request headers the answer depends on, in canonical
	// form and without duplicates. The caller may keep or change the slice.
	Vary() []string
}

// maxTagLength bounds what a query parameter, cookie or host can ask for.
// Real language tags are far shorter; anything longer cannot be registered.
const maxTagLength = 64

// registeredLocale returns the registered spelling of the catalog tag names
// (exactly, or by its base language), or "".
func registeredLocale(tag string) string {
	tag = normalizeLocale(tag)
	if tag == "" || len(tag) > maxTagLength {
		return ""
	}
	catalogsMu.RLock()
	defer catalogsMu.RUnlock()
	return matchTagLocked(tag)
}

// matchTagLocked is registeredLocale for a trimmed tag; the caller holds
// catalogsMu.
func matchTagLocked(tag string) string {
	if name, ok := catalogName(tag); ok {
		return name
	}
	if base := baseLanguage(tag); base != "" && base != tag {
		if name, ok := catalogName(base); ok {
			return name
		}
	}
	return ""
}

type queryResolver struct{ param string }

// FromQuery resolves the locale named by the URL query parameter param, for
// a link or form that asks for a language for one request. A value without a
// registered catalog resolves nothing.
func FromQuery(param string) Resolver { return queryResolver{param} }

func (q queryResolver) Resolve(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return registeredLocale(r.URL.Query().Get(q.param))
}

func (queryResolver) Vary() []string { return nil }

type cookieResolver struct{ name string }

// FromCookie resolves the locale saved in the cookie called name. A value
// without a registered catalog resolves nothing. The host sets the cookie;
// this package never does.
func FromCookie(name string) Resolver { return cookieResolver{name} }

func (c cookieResolver) Resolve(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(c.name)
	if err != nil {
		return ""
	}
	return registeredLocale(cookie.Value)
}

func (cookieResolver) Vary() []string { return []string{"Cookie"} }

type hostResolver struct{ locales map[string]string }

// FromHost resolves the locale a host name stands for, such as
// "es.example.com" to "es". The map is copied, and its keys compared in lower
// case without port or trailing dot, so later changes by the caller change
// nothing. The locale is checked against the catalogs on every request, so
// catalogs may be registered after the resolver is built.
func FromHost(hosts map[string]string) Resolver {
	locales := make(map[string]string, len(hosts))
	for host, locale := range hosts {
		if host = normalizeHost(host); host != "" {
			locales[host] = locale
		}
	}
	return hostResolver{locales}
}

func (h hostResolver) Resolve(r *http.Request) string {
	if r == nil {
		return ""
	}
	locale, ok := h.locales[normalizeHost(r.Host)]
	if !ok {
		return ""
	}
	return registeredLocale(locale)
}

// Vary is empty: the host is already part of what a cache keys on.
func (hostResolver) Vary() []string { return nil }

// normalizeHost lower-cases a host and drops its port, brackets around an IPv6
// literal and a trailing dot.
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case strings.HasPrefix(host, "["):
		if end := strings.IndexByte(host, ']'); end > 0 {
			host = host[1:end]
		}
	case strings.Count(host, ":") == 1:
		host, _, _ = strings.Cut(host, ":")
	}
	return strings.TrimSuffix(host, ".")
}

type funcResolver struct {
	fn   LocaleResolver
	vary []string
}

// FuncResolver adapts a plain function, for a locale source this package has
// no helper for (a session, an account preference, a path prefix). A plain
// function cannot say what it depends on, so name the request headers it
// reads in vary (for instance "Cookie"); they are normalized and
// de-duplicated. With none, no Vary header is added for it.
func FuncResolver(f LocaleResolver, vary ...string) Resolver {
	return funcResolver{fn: f, vary: normalizeVary(vary)}
}

func (f funcResolver) Resolve(r *http.Request) string {
	if f.fn == nil || r == nil {
		return ""
	}
	return f.fn(r)
}

func (f funcResolver) Vary() []string { return append([]string(nil), f.vary...) }

type chainResolver struct {
	resolvers []Resolver
	vary      []string
}

// Chain tries resolvers in the order given and returns the first answer that
// has a registered catalog (a resolver that answers a locale nobody
// translated is skipped), then falls back to the Accept-Language header. Its
// Vary is the union of the resolvers' and Accept-Language, so a cache in
// front of the application keeps one copy per language.
func Chain(resolvers ...Resolver) Resolver {
	kept := make([]Resolver, 0, len(resolvers))
	var vary []string
	for _, r := range resolvers {
		if r == nil {
			continue
		}
		kept = append(kept, r)
		vary = append(vary, r.Vary()...)
	}
	vary = append(vary, "Accept-Language")
	return chainResolver{resolvers: kept, vary: normalizeVary(vary)}
}

func (c chainResolver) Resolve(r *http.Request) string {
	for _, resolver := range c.resolvers {
		if locale := registeredLocale(resolver.Resolve(r)); locale != "" {
			return locale
		}
	}
	return DefaultLocaleResolver(r)
}

func (c chainResolver) Vary() []string { return append([]string(nil), c.vary...) }

// acceptLanguage is the built-in resolver behind Middleware(nil).
type acceptLanguage struct{}

func (acceptLanguage) Resolve(r *http.Request) string { return DefaultLocaleResolver(r) }
func (acceptLanguage) Vary() []string                 { return []string{"Accept-Language"} }

// MiddlewareResolver is Middleware for a Resolver: it stores the resolved
// locale on the request's context, adds the resolver's Vary tokens to every
// response (merged with any Vary header already present, ignoring case and
// never repeating a token, and leaving "*" alone), and sets Content-Language
// to the selected catalog's locale when there is one and nothing else has set
// it. A nil resolver uses the Accept-Language header.
func MiddlewareResolver(resolver Resolver) tango.Middleware {
	if resolver == nil {
		resolver = acceptLanguage{}
	}
	vary := normalizeVary(resolver.Vary())
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			mergeVary(header, vary)
			writer := &varyWriter{ResponseWriter: w, vary: vary}
			defer writer.finish()
			if locale := resolver.Resolve(r); locale != "" {
				r = r.WithContext(WithLocale(r.Context(), locale))
				if header.Get("Content-Language") == "" {
					if selected := registeredLocale(locale); selected != "" {
						header.Set("Content-Language", selected)
					}
				}
			}
			next.ServeHTTP(writer, r)
		})
	}
}

// mergeVary makes the response's Vary header carry tokens and every token it
// already had, each once (compared without regard to case). A "*" already
// present says the response varies on everything, so the header is left as it
// is. It runs again when the response is written, so a Vary that a handler
// adds after the middleware ran is merged too.
func mergeVary(header http.Header, tokens []string) {
	lines := header.Values("Vary")
	var merged []string
	have := make(map[string]bool)
	add := func(token string) {
		token = strings.TrimSpace(token)
		if token != "" && !have[strings.ToLower(token)] {
			have[strings.ToLower(token)] = true
			merged = append(merged, token)
		}
	}
	for _, line := range lines {
		for _, token := range strings.Split(line, ",") {
			if strings.TrimSpace(token) == "*" {
				return
			}
			add(token)
		}
	}
	for _, token := range tokens {
		add(token)
	}
	if len(merged) == 0 {
		return
	}
	value := strings.Join(merged, ", ")
	if len(lines) == 1 && lines[0] == value {
		return
	}
	header.Set("Vary", value)
}

// varyWriter merges the resolver's Vary tokens into the header just before it
// is sent, whatever the downstream handler added in between.
type varyWriter struct {
	http.ResponseWriter
	vary    []string
	flushed bool
}

func (w *varyWriter) finish() {
	if !w.flushed {
		w.flushed = true
		mergeVary(w.ResponseWriter.Header(), w.vary)
	}
}

func (w *varyWriter) WriteHeader(status int) {
	w.finish()
	w.ResponseWriter.WriteHeader(status)
}

func (w *varyWriter) Write(b []byte) (int, error) {
	w.finish()
	return w.ResponseWriter.Write(b)
}

func (w *varyWriter) Flush() {
	w.finish()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack keeps connection upgrades (WebSockets) working behind the middleware.
func (w *varyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	w.finish()
	return h.Hijack()
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *varyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// normalizeVary returns tokens in canonical header form, comma lists split,
// blanks dropped and case-insensitive duplicates removed, keeping the first
// occurrence's position. "*" is kept as it is.
func normalizeVary(tokens []string) []string {
	var out []string
	seen := make(map[string]bool, len(tokens))
	for _, list := range tokens {
		for _, token := range strings.Split(list, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if token != "*" {
				token = http.CanonicalHeaderKey(token)
			}
			if key := strings.ToLower(token); !seen[key] {
				seen[key] = true
				out = append(out, token)
			}
		}
	}
	return out
}
