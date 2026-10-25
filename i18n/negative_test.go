package i18n

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocaleWithoutContextValue(t *testing.T) {
	var nilCtx context.Context
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"nil context", nilCtx, ""},
		{"background context", context.Background(), ""},
		{"context with locale", WithLocale(context.Background(), "fr"), "fr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Locale(tt.ctx); got != tt.want {
				t.Fatalf("Locale = %q; want %q", got, tt.want)
			}
		})
	}
}

func TestRegisterCatalogIgnoresEmptyInput(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	tests := []struct {
		name    string
		locale  string
		catalog map[string]string
	}{
		{"empty locale", "", map[string]string{"a": "b"}},
		{"nil catalog", "xx-nil", nil},
		{"empty catalog", "xx-empty", map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterCatalog(tt.locale, tt.catalog)
			if n, c := catalogSize(); n != 0 || c != 0 {
				t.Fatalf("catalogs after no-op registration: %d locales, %d canonical; want 0, 0", n, c)
			}
		})
	}
}

func TestDefaultLocaleResolverNegotiation(t *testing.T) {
	outer := isolate()
	t.Cleanup(outer)
	RegisterCatalog("de-neg", map[string]string{"k": "v"})
	RegisterCatalog("fr-neg", map[string]string{"k": "v"})

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"header absent", "", ""},
		{"wildcard only", "*", ""},
		{"empty parts", " , ,", ""},
		{"zero quality excluded", "de-neg;q=0", ""},
		{"invalid quality is zero", "de-neg;q=x", ""},
		{"zero quality skipped for next", "de-neg;q=0, fr-neg;q=0.1", "fr-neg"},
		{"higher weight wins", "de-neg;q=0.2, fr-neg;q=0.9", "fr-neg"},
		{"equal weight keeps header order", "fr-neg;q=0.5, de-neg;q=0.5", "fr-neg"},
		{"unregistered locale", "es-neg", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set("Accept-Language", tt.header)
			if got := DefaultLocaleResolver(r); got != tt.want {
				t.Fatalf("DefaultLocaleResolver(%q) = %q; want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestDefaultLocaleResolverNilRequest(t *testing.T) {
	if got := DefaultLocaleResolver(nil); got != "" {
		t.Fatalf("DefaultLocaleResolver(nil) = %q; want empty", got)
	}
}

func TestResolversAnswerNothingWithoutRequest(t *testing.T) {
	calls := 0
	tests := []struct {
		name     string
		resolver Resolver
		req      *http.Request
	}{
		{"query nil request", FromQuery("lang"), nil},
		{"query nil URL", FromQuery("lang"), &http.Request{}},
		{"cookie nil request", FromCookie("lang"), nil},
		{"host nil request", FromHost(map[string]string{"example.com": "fr"}), nil},
		{"func nil request", FuncResolver(func(*http.Request) string { calls++; return "fr" }), nil},
		{"func nil function", FuncResolver(nil), httptest.NewRequest(http.MethodGet, "/", nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resolver.Resolve(tt.req); got != "" {
				t.Fatalf("Resolve = %q; want empty", got)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("function resolver called with nil request %d times; want 0", calls)
	}
}

func TestVaryWriterFlush(t *testing.T) {
	tests := []struct {
		name        string
		underlying  func(rec *httptest.ResponseRecorder) http.ResponseWriter
		wantFlushed bool
	}{
		{"flusher underlying", func(rec *httptest.ResponseRecorder) http.ResponseWriter { return rec }, true},
		{"non-flusher underlying", func(rec *httptest.ResponseRecorder) http.ResponseWriter {
			return struct{ http.ResponseWriter }{rec}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := &varyWriter{ResponseWriter: tt.underlying(rec), vary: []string{"Cookie"}}
			w.Flush()
			if rec.Flushed != tt.wantFlushed {
				t.Fatalf("Flushed = %v; want %v", rec.Flushed, tt.wantFlushed)
			}
			if got := rec.Header().Get("Vary"); got != "Cookie" {
				t.Fatalf("Vary = %q; want %q", got, "Cookie")
			}
		})
	}
}

type hijackingWriter struct {
	http.ResponseWriter
	err error
}

func (h hijackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, h.err
}

func TestVaryWriterHijack(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name       string
		underlying http.ResponseWriter
		wantErr    error
	}{
		{"underlying cannot hijack", struct{ http.ResponseWriter }{httptest.NewRecorder()}, http.ErrNotSupported},
		{"underlying hijack error passes through", hijackingWriter{httptest.NewRecorder(), errBoom}, errBoom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &varyWriter{ResponseWriter: tt.underlying, vary: []string{"Cookie"}}
			_, _, err := w.Hijack()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Hijack error = %v; want %v", err, tt.wantErr)
			}
		})
	}
}

func TestVaryWriterUnwrapReturnsUnderlying(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &varyWriter{ResponseWriter: rec}
	if got := w.Unwrap(); got != http.ResponseWriter(rec) {
		t.Fatalf("Unwrap returned %T; want the recorder", got)
	}
}

func TestVerbSetOrdersAndDeduplicatesUses(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   []verbUse
		ok     bool
	}{
		{"plain text", "hello", nil, true},
		{"escaped percent", "100%%", nil, true},
		{"same argument, verbs ordered", "%[1]s %[1]d", []verbUse{{0, 'd'}, {0, 's'}}, true},
		{"duplicate use compacted", "%[1]s %[1]s", []verbUse{{0, 's'}}, true},
		{"indexed arguments sorted", "%[2]s %[1]d", []verbUse{{0, 'd'}, {1, 's'}}, true},
		{"star width consumes argument", "%*d", []verbUse{{0, '*'}, {1, 'd'}}, true},
		{"star precision consumes argument", "%.*d", []verbUse{{0, '*'}, {1, 'd'}}, true},
		{"unclosed bracket index", "%[1d", nil, false},
		{"non-numeric bracket index", "%.[x]d", nil, false},
		{"zero bracket index", "%[0]d", nil, false},
		{"empty bracket index", "%[]d", nil, false},
		{"missing verb after index", "%[1]", nil, false},
		{"unknown verb", "%y", nil, false},
		{"trailing percent", "abc%", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := verbSet(tt.format)
			if ok != tt.ok {
				t.Fatalf("verbSet(%q) ok = %v; want %v", tt.format, ok, tt.ok)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("verbSet(%q) = %v; want %v", tt.format, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("verbSet(%q) = %v; want %v", tt.format, got, tt.want)
				}
			}
		})
	}
}

func TestUsableRejectsMismatchedVerbs(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     bool
	}{
		{"empty value", "", "hello", false},
		{"no verbs on either side", "hola", "hello", true},
		{"same verbs", "hola %s", "hello %s", true},
		{"verb type differs", "hola %d", "hello %s", false},
		{"value drops a verb", "hola", "hello %s", false},
		{"value adds a verb", "hola %s", "hello", false},
		{"malformed value", "hola %y", "hello %s", false},
		{"malformed fallback trusts translation", "hola %d", "hello %y", true},
		{"repeated lookup uses cache", "hola %s", "hello %s", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 2 {
				if got := usable(tt.value, tt.fallback); got != tt.want {
					t.Fatalf("usable(%q, %q) = %v; want %v", tt.value, tt.fallback, got, tt.want)
				}
			}
		})
	}
}
