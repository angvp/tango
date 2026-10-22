package i18n

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// serve runs mw around a handler and returns the response headers.
func serve(mw func(http.Handler) http.Handler, r *http.Request, prepare func(http.Header), inner func(http.ResponseWriter, *http.Request)) http.Header {
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inner != nil {
			inner(w, r)
		}
	}))
	rec := httptest.NewRecorder()
	if prepare != nil {
		prepare(rec.Header())
	}
	handler.ServeHTTP(rec, r)
	return rec.Result().Header
}

func varyTokens(h http.Header) []string {
	var out []string
	for _, line := range h.Values("Vary") {
		for _, token := range strings.Split(line, ",") {
			if token = strings.TrimSpace(token); token != "" {
				out = append(out, token)
			}
		}
	}
	return out
}

func TestMiddlewareNilVariesOnAcceptLanguage(t *testing.T) {
	registerSpanish()
	mw := Middleware(nil)

	h := serve(mw, request("/", func(r *http.Request) { r.Header.Set("Accept-Language", "ES-mx") }), nil, nil)
	if got := varyTokens(h); !reflect.DeepEqual(got, []string{"Accept-Language"}) {
		t.Errorf("Vary = %v", got)
	}
	if got := h.Get("Content-Language"); got != "es-MX" {
		t.Errorf("Content-Language = %q, want the catalog's spelling es-MX", got)
	}

	// A request that resolves nothing still varies, and states no language.
	h = serve(mw, request("/", func(r *http.Request) { r.Header.Set("Accept-Language", "ja") }), nil, nil)
	if got := varyTokens(h); !reflect.DeepEqual(got, []string{"Accept-Language"}) {
		t.Errorf("unresolved request Vary = %v", got)
	}
	if got := h.Get("Content-Language"); got != "" {
		t.Errorf("Content-Language = %q for an untranslated request", got)
	}
}

func TestMiddlewareWithAPlainFunctionAddsNoVary(t *testing.T) {
	registerSpanish()
	mw := Middleware(func(*http.Request) string { return "fr" })
	h := serve(mw, request("/", nil), nil, nil)
	if got := varyTokens(h); len(got) != 0 {
		t.Errorf("Vary = %v, a plain function declares nothing", got)
	}
	if got := h.Get("Content-Language"); got != "fr" {
		t.Errorf("Content-Language = %q, want fr", got)
	}

	// A locale with no catalog is still stored but is not announced.
	var seen string
	mw = Middleware(func(*http.Request) string { return "zz" })
	h = serve(mw, request("/", nil), nil, func(_ http.ResponseWriter, r *http.Request) { seen = Locale(r.Context()) })
	if h.Get("Content-Language") != "" || seen != "zz" {
		t.Errorf("Content-Language = %q, context locale = %q", h.Get("Content-Language"), seen)
	}
}

func TestChainSendsItsOwnVaryTokens(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromQuery("lang"), FromCookie("lang")))
	h := serve(mw, request("/?lang=fr", nil), nil, nil)
	if got := varyTokens(h); !reflect.DeepEqual(got, []string{"Cookie", "Accept-Language"}) {
		t.Errorf("Vary = %v", got)
	}
	if got := h.Get("Content-Language"); got != "fr" {
		t.Errorf("Content-Language = %q", got)
	}

	mw = MiddlewareResolver(Chain(FromHost(map[string]string{"es.example.com": "es"})))
	h = serve(mw, request("/", func(r *http.Request) { r.Host = "es.example.com" }), nil, nil)
	if got := varyTokens(h); !reflect.DeepEqual(got, []string{"Accept-Language"}) {
		t.Errorf("host-only chain Vary = %v, the host needs no token", got)
	}
}

func TestVaryMergesWithExistingValuesWithoutDuplicates(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromCookie("lang")))
	cases := []struct {
		name     string
		existing []string
		want     []string
	}{
		{"none", nil, []string{"Cookie", "Accept-Language"}},
		{"unrelated", []string{"Origin"}, []string{"Origin", "Cookie", "Accept-Language"}},
		{"different case", []string{"cookie, ACCEPT-LANGUAGE"}, []string{"cookie", "ACCEPT-LANGUAGE"}},
		{"several lines", []string{"Accept-Encoding", "cookie"}, []string{"Accept-Encoding", "cookie", "Accept-Language"}},
		{"wildcard is left alone", []string{"*"}, []string{"*"}},
		{"wildcard among others", []string{"Origin, *"}, []string{"Origin", "*"}},
	}
	for _, tc := range cases {
		h := serve(mw, request("/", nil), func(h http.Header) {
			for _, v := range tc.existing {
				h.Add("Vary", v)
			}
		}, nil)
		if got := varyTokens(h); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: Vary = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestVaryAddedByTheHandlerAfterwardsIsKept(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromCookie("lang")))
	h := serve(mw, request("/", nil), nil, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Origin")
	})
	if got := varyTokens(h); !reflect.DeepEqual(got, []string{"Cookie", "Accept-Language", "Origin"}) {
		t.Errorf("Vary = %v, want ours and the handler's", got)
	}
}

func TestVaryAddedByTheHandlerInAnotherCaseIsNotRepeated(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromCookie("lang")))
	for name, inner := range map[string]func(http.ResponseWriter, *http.Request){
		"header only": func(w http.ResponseWriter, _ *http.Request) { w.Header().Add("Vary", "cookie") },
		"before WriteHeader": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("vary", "COOKIE, Origin")
			w.WriteHeader(http.StatusTeapot)
		},
		"before Write": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("Vary", "cookie")
			_, _ = w.Write([]byte("x"))
		},
	} {
		h := serve(mw, request("/", nil), nil, inner)
		seen := map[string]int{}
		for _, token := range varyTokens(h) {
			seen[strings.ToLower(token)]++
		}
		for token, n := range seen {
			if n != 1 {
				t.Errorf("%s: %q appears %d times in %v", name, token, n, h.Values("Vary"))
			}
		}
		if seen["cookie"] != 1 || seen["accept-language"] != 1 {
			t.Errorf("%s: Vary = %v, want Cookie and Accept-Language once each", name, h.Values("Vary"))
		}
	}
}

func TestContentLanguageOnlyWhenACatalogWasSelectedAndNeverOverridden(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromQuery("lang")))

	h := serve(mw, request("/?lang=es_mx", nil), nil, nil)
	if got := h.Get("Content-Language"); got != "es-MX" {
		t.Errorf("Content-Language = %q, want the registered spelling", got)
	}

	h = serve(mw, request("/?lang=es-AR", nil), nil, nil)
	if got := h.Get("Content-Language"); got != "es" {
		t.Errorf("Content-Language = %q, the base-language catalog was selected", got)
	}

	h = serve(mw, request("/", nil), nil, nil)
	if got := h.Get("Content-Language"); got != "" {
		t.Errorf("Content-Language = %q with no locale", got)
	}

	h = serve(mw, request("/?lang=fr", nil), func(h http.Header) { h.Set("Content-Language", "de") }, nil)
	if got := h.Values("Content-Language"); !reflect.DeepEqual(got, []string{"de"}) {
		t.Errorf("an existing Content-Language was changed: %v", got)
	}
}

func TestHeadersAreSetBeforeTheHandlerWrites(t *testing.T) {
	registerSpanish()
	mw := MiddlewareResolver(Chain(FromQuery("lang")))
	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w.Header().Get("Content-Language") != "fr" || len(varyTokens(w.Header())) == 0 {
			t.Errorf("headers not yet set when the handler runs: %v", w.Header())
		}
		w.WriteHeader(http.StatusAccepted)
	})).ServeHTTP(rec, request("/?lang=fr", nil))
	if rec.Code != http.StatusAccepted || rec.Header().Get("Content-Language") != "fr" {
		t.Errorf("status %d, headers %v", rec.Code, rec.Header())
	}
}
