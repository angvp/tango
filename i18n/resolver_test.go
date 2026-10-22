package i18n

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func registerSpanish() {
	resetCatalogsForTest()
	RegisterCatalog("es", map[string]string{"k": "v"})
	RegisterCatalog("es-MX", map[string]string{"k": "v"})
	RegisterCatalog("fr", map[string]string{"k": "v"})
}

func request(target string, edit func(*http.Request)) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	if edit != nil {
		edit(r)
	}
	return r
}

func TestFromQueryReturnsOnlyRegisteredLocales(t *testing.T) {
	registerSpanish()
	resolver := FromQuery("lang")
	cases := map[string]string{
		"/?lang=es":                          "es",
		"/?lang=ES_mx":                       "es-MX",
		"/?lang=es-AR":                       "es", // base language
		"/?lang=xx":                          "",
		"/?lang=":                            "",
		"/":                                  "",
		"/?other=es":                         "",
		"/?lang=" + strings.Repeat("a", 500): "",
		"/?lang=es&lang=fr":                  "es", // the first value
	}
	for target, want := range cases {
		if got := resolver.Resolve(request(target, nil)); got != want {
			t.Errorf("Resolve(%.40q) = %q, want %q", target, got, want)
		}
	}
	if vary := resolver.Vary(); len(vary) != 0 {
		t.Errorf("Vary() = %v, a query parameter is part of the URL and needs none", vary)
	}
}

func TestFromCookieReturnsOnlyRegisteredLocales(t *testing.T) {
	registerSpanish()
	resolver := FromCookie("lang")
	withCookie := func(value string) *http.Request {
		return request("/", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "lang", Value: value}) })
	}
	cases := map[string]string{"fr": "fr", "es_mx": "es-MX", "xx": "", "": "", strings.Repeat("a", 500): ""}
	for value, want := range cases {
		if got := resolver.Resolve(withCookie(value)); got != want {
			t.Errorf("cookie %.20q resolved %q, want %q", value, got, want)
		}
	}
	if got := resolver.Resolve(request("/", nil)); got != "" {
		t.Errorf("no cookie resolved %q", got)
	}
	if got, want := resolver.Vary(), []string{"Cookie"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vary() = %v, want %v", got, want)
	}
}

func TestFromHostNormalizesAndCopiesItsMap(t *testing.T) {
	registerSpanish()
	hosts := map[string]string{
		"ES.Example.com:8443": "es",
		"fr.example.com.":     "fr",
		"[::1]:3000":          "es-MX",
		"xx.example.com":      "xx", // unregistered locale
	}
	resolver := FromHost(hosts)
	hosts["fr.example.com"] = "es" // later changes to the host's map change nothing
	delete(hosts, "ES.Example.com:8443")

	cases := map[string]string{
		"es.example.com":       "es",
		"ES.EXAMPLE.COM:80":    "es",
		"fr.example.com":       "fr",
		"fr.example.com.:8080": "fr",
		"[::1]:3000":           "es-MX",
		"[::1]":                "es-MX",
		"xx.example.com":       "",
		"other.example.com":    "",
		"":                     "",
	}
	for host, want := range cases {
		r := request("/", func(r *http.Request) { r.Host = host })
		if got := resolver.Resolve(r); got != want {
			t.Errorf("Host %q resolved %q, want %q", host, got, want)
		}
	}
	if vary := resolver.Vary(); len(vary) != 0 {
		t.Errorf("Vary() = %v, the host is already part of a cache key", vary)
	}
}

func TestFromHostFollowsCatalogsRegisteredAfterConstruction(t *testing.T) {
	resetCatalogsForTest()
	resolver := FromHost(map[string]string{"de.example.com": "de"})
	r := request("/", func(r *http.Request) { r.Host = "de.example.com" })
	if got := resolver.Resolve(r); got != "" {
		t.Fatalf("resolved %q before the catalog existed", got)
	}
	RegisterCatalog("de", map[string]string{"k": "v"})
	if got := resolver.Resolve(r); got != "de" {
		t.Errorf("resolved %q after registering de, want de", got)
	}
}

func TestChainTriesResolversInTheHostsOrderThenAcceptLanguage(t *testing.T) {
	registerSpanish()
	chain := Chain(FromQuery("lang"), FromCookie("lang"))
	build := func(query, cookie, accept string) *http.Request {
		target := "/"
		if query != "" {
			target += "?lang=" + query
		}
		return request(target, func(r *http.Request) {
			if cookie != "" {
				r.AddCookie(&http.Cookie{Name: "lang", Value: cookie})
			}
			if accept != "" {
				r.Header.Set("Accept-Language", accept)
			}
		})
	}
	cases := []struct{ name, query, cookie, accept, want string }{
		{"query beats cookie", "fr", "es", "es-MX", "fr"},
		{"cookie beats Accept-Language", "", "fr", "es", "fr"},
		{"an unregistered query value falls through", "xx", "fr", "", "fr"},
		{"an unregistered cookie falls through to Accept-Language", "", "xx", "es-MX", "es-MX"},
		{"Accept-Language is the last fallback", "", "", "FR-ca", "fr"},
		{"nothing resolves", "", "", "ja", ""},
	}
	for _, tc := range cases {
		if got := chain.Resolve(build(tc.query, tc.cookie, tc.accept)); got != tc.want {
			t.Errorf("%s: resolved %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestChainSkipsFunctionResolversThatReturnUnregisteredLocales(t *testing.T) {
	registerSpanish()
	always := func(locale string) Resolver {
		return FuncResolver(func(*http.Request) string { return locale })
	}
	if got := Chain(always("zz"), always("ES_mx")).Resolve(request("/", nil)); got != "es-MX" {
		t.Errorf("resolved %q, want the first registered locale in its registered spelling", got)
	}
	if got := Chain(nil, always("")).Resolve(request("/", nil)); got != "" {
		t.Errorf("nil and empty resolvers resolved %q", got)
	}
}

func TestChainOwnsItsVaryTokens(t *testing.T) {
	cases := []struct {
		name  string
		chain Resolver
		want  []string
	}{
		{"cookie then the built-in fallback", Chain(FromQuery("lang"), FromCookie("lang")), []string{"Cookie", "Accept-Language"}},
		{"host adds nothing", Chain(FromHost(map[string]string{"a": "es"})), []string{"Accept-Language"}},
		{"function tokens are normalized and de-duplicated", Chain(
			FuncResolver(nil, " cookie ", "X-Region, accept-language", "COOKIE", ""),
		), []string{"Cookie", "X-Region", "Accept-Language"}},
		{"empty chain", Chain(), []string{"Accept-Language"}},
	}
	for _, tc := range cases {
		if got := tc.chain.Vary(); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Vary() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVaryReturnsACopy(t *testing.T) {
	resolver := FuncResolver(nil, "Cookie")
	resolver.Vary()[0] = "Tampered"
	if got := resolver.Vary(); got[0] != "Cookie" {
		t.Errorf("Vary() exposes internal state: %v", got)
	}
}

func TestMiddlewareResolverStoresTheResolvedLocale(t *testing.T) {
	registerSpanish()
	var seen string
	handler := MiddlewareResolver(Chain(FromQuery("lang")))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = Locale(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, request("/?lang=es_mx", nil))
	if seen != "es-MX" {
		t.Errorf("handler saw locale %q, want es-MX", seen)
	}
	if cookies := rec.Result().Cookies(); len(cookies) != 0 {
		t.Errorf("a resolver set cookies: %v", cookies)
	}

	handler.ServeHTTP(httptest.NewRecorder(), request("/?lang=xx", nil))
	if seen != "" {
		t.Errorf("an unresolved request carried locale %q", seen)
	}
}

func TestMiddlewareResolverNilUsesAcceptLanguage(t *testing.T) {
	registerSpanish()
	var seen string
	handler := MiddlewareResolver(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = Locale(r.Context())
	}))
	handler.ServeHTTP(httptest.NewRecorder(), request("/", func(r *http.Request) { r.Header.Set("Accept-Language", "FR") }))
	if seen != "fr" {
		t.Errorf("handler saw %q, want fr", seen)
	}
}
