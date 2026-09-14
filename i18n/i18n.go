// Package i18n provides tanGO's small text-translation primitives.
//
// It is intentionally an override layer: callers keep English fallback text
// inline in source, while registered catalogs provide translated replacements
// for stable symbolic keys.
package i18n

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/angvp/tango"
)

type localeContextKey struct{}

var (
	catalogsMu sync.RWMutex
	catalogs   = make(map[string]map[string]string)
)

// WithLocale returns a child context carrying locale as the resolved locale
// for translation lookups.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, localeContextKey{}, locale)
}

// Locale returns the resolved locale carried by ctx, or "" when none is set.
func Locale(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	locale, _ := ctx.Value(localeContextKey{}).(string)
	return locale
}

// RegisterCatalog merges catalog into the existing catalog for locale.
// Registering the same key again replaces that key's previous value; other
// keys and other locales are left untouched.
func RegisterCatalog(locale string, catalog map[string]string) {
	if locale == "" || len(catalog) == 0 {
		return
	}

	catalogsMu.Lock()
	defer catalogsMu.Unlock()

	dest := catalogs[locale]
	if dest == nil {
		dest = make(map[string]string, len(catalog))
		catalogs[locale] = dest
	}
	for key, value := range catalog {
		dest[key] = value
	}
}

// LocaleResolver resolves the active locale for one incoming request.
type LocaleResolver func(*http.Request) string

// DefaultLocaleResolver resolves the request locale from Accept-Language,
// using the registered catalogs as the set of supported locales.
func DefaultLocaleResolver(r *http.Request) string {
	if r == nil {
		return ""
	}
	return resolveAcceptLanguage(r.Header.Get("Accept-Language"))
}

// Middleware stores the locale resolved for each request on the request's
// context so later T calls can observe it. A nil resolver uses
// DefaultLocaleResolver.
func Middleware(resolver LocaleResolver) tango.Middleware {
	if resolver == nil {
		resolver = DefaultLocaleResolver
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			locale := resolver(r)
			if locale != "" {
				r = r.WithContext(WithLocale(r.Context(), locale))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// T returns the translated string for key using the locale carried by ctx,
// falling back to fallback when no locale/catalog/key is available. When args
// are present, the chosen string is formatted with fmt.Sprintf before being
// returned.
func T(ctx context.Context, key, fallback string, args ...any) string {
	chosen := lookup(Locale(ctx), key, fallback)
	if len(args) > 0 {
		return fmt.Sprintf(chosen, args...)
	}
	return chosen
}

func lookup(locale, key, fallback string) string {
	if locale == "" || key == "" {
		return fallback
	}

	catalogsMu.RLock()
	defer catalogsMu.RUnlock()

	if value, ok := catalogValue(locale, key); ok {
		return value
	}
	if base := baseLanguage(locale); base != "" && base != locale {
		if value, ok := catalogValue(base, key); ok {
			return value
		}
	}
	return fallback
}

func catalogValue(locale, key string) (string, bool) {
	catalog, ok := catalogs[locale]
	if !ok {
		return "", false
	}
	value, ok := catalog[key]
	return value, ok
}

func catalogExists(locale string) bool {
	_, ok := catalogs[locale]
	return ok
}

func resolveAcceptLanguage(header string) string {
	candidates := parseAcceptLanguage(header)
	if len(candidates) == 0 {
		return ""
	}

	catalogsMu.RLock()
	defer catalogsMu.RUnlock()

	for _, candidate := range candidates {
		tag := normalizeLocale(candidate.tag)
		if tag == "" {
			continue
		}
		if catalogExists(tag) {
			return tag
		}
		if base := baseLanguage(tag); base != "" && base != tag && catalogExists(base) {
			return base
		}
	}
	return ""
}

type languageCandidate struct {
	tag   string
	q     float64
	order int
}

func parseAcceptLanguage(header string) []languageCandidate {
	var candidates []languageCandidate
	for order, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, params, _ := strings.Cut(part, ";")
		tag = normalizeLocale(tag)
		if tag == "" || tag == "*" {
			continue
		}
		q := 1.0
		for _, param := range strings.Split(params, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || strings.TrimSpace(name) != "q" {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				q = 0
				break
			}
			q = parsed
		}
		if q <= 0 {
			continue
		}
		candidates = append(candidates, languageCandidate{tag: tag, q: q, order: order})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q == candidates[j].q {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].q > candidates[j].q
	})
	return candidates
}

func normalizeLocale(locale string) string {
	return strings.TrimSpace(locale)
}

func baseLanguage(locale string) string {
	if i := strings.IndexAny(locale, "-_"); i > 0 {
		return locale[:i]
	}
	return locale
}

func resetCatalogsForTest() {
	catalogsMu.Lock()
	defer catalogsMu.Unlock()
	catalogs = make(map[string]map[string]string)
}
