package tango_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
)

// serveThroughSecurityHeaders wraps a handler that writes 200 with mw and
// returns the response it produced for one GET request.
func serveThroughSecurityHeaders(t *testing.T, mw tango.Middleware) *httptest.ResponseRecorder {
	t.Helper()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	return recorder
}

func TestSecurityHeadersSetsNoSniffOnEveryResponse(t *testing.T) {
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders())

	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestSecurityHeadersEnabledWithoutPolicyAppliesStrictBaselineAndCompanionDefaults(t *testing.T) {
	restore := tango.SetNonceRandomSourceForTest(zeroRandomSource{})
	defer restore()
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders())

	const wantCSP = "default-src 'self'; script-src 'self' 'nonce-AAAAAAAAAAAAAAAAAAAAAA=='; style-src 'self' 'nonce-AAAAAAAAAAAAAAAAAAAAAA=='; object-src 'none'; base-uri 'self'; frame-ancestors 'self'"
	if got := response.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "strict-origin-when-cross-origin")
	}
	if got := response.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("X-Frame-Options = %q, want %q", got, "SAMEORIGIN")
	}
}

func TestSecurityHeadersStrictCSPOptionSendsTheSameHeaderAsNoOption(t *testing.T) {
	restore := tango.SetNonceRandomSourceForTest(zeroRandomSource{})
	defer restore()
	implicit := serveThroughSecurityHeaders(t, tango.SecurityHeaders())
	explicit := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithContentSecurityPolicy(tango.StrictCSP())))

	if got, want := explicit.Header().Get("Content-Security-Policy"), implicit.Header().Get("Content-Security-Policy"); got != want {
		t.Fatalf("Content-Security-Policy with StrictCSP() = %q, want the implicit baseline %q", got, want)
	}
	if got := explicit.Header().Get("Content-Security-Policy"); !strings.Contains(got, "script-src 'self' 'nonce-AAAAAAAAAAAAAAAAAAAAAA=='") {
		t.Fatalf("Content-Security-Policy = %q, want the nonce-bearing baseline", got)
	}
}

func TestSecurityHeadersRendersSuppliedPolicyInFixedOrder(t *testing.T) {
	policy := tango.CSP{
		DefaultSrc:     []tango.Source{tango.Self()},
		ScriptSrc:      []tango.Source{tango.Self(), tango.Host("https://cdn.example.com")},
		StyleSrc:       []tango.Source{tango.Self(), tango.Host("fonts.example.com")},
		ImgSrc:         []tango.Source{tango.Self(), tango.Data(), tango.Blob()},
		FontSrc:        []tango.Source{tango.Scheme("https")},
		ConnectSrc:     []tango.Source{tango.Self()},
		FormAction:     []tango.Source{tango.Self()},
		ObjectSrc:      []tango.Source{tango.None()},
		BaseURI:        []tango.Source{tango.None()},
		FrameAncestors: []tango.Source{tango.None()},
	}
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)))

	const wantCSP = "default-src 'self'; script-src 'self' https://cdn.example.com; style-src 'self' fonts.example.com; img-src 'self' data: blob:; font-src https:; connect-src 'self'; form-action 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'"
	if got := response.Header().Get("Content-Security-Policy"); got != wantCSP {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, wantCSP)
	}
	if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want %q", got, "DENY")
	}
}

func TestSecurityHeadersSuppliedPolicyIsUsedAsWrittenWithoutBaselineMerge(t *testing.T) {
	policy := tango.CSP{ImgSrc: []tango.Source{tango.Self()}}
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)))

	if got := response.Header().Get("Content-Security-Policy"); got != "img-src 'self'" {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, "img-src 'self'")
	}
	if got := response.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want %q (frame-ancestors absent)", got, "DENY")
	}
}

func TestSecurityHeadersUnsafeInlineStyleRendersAsUnsafeInlineInStyleSrc(t *testing.T) {
	policy := tango.CSP{StyleSrc: []tango.Source{tango.Self(), tango.UnsafeInlineStyle()}}
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)))

	if got := response.Header().Get("Content-Security-Policy"); got != "style-src 'self' 'unsafe-inline'" {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, "style-src 'self' 'unsafe-inline'")
	}
}

func TestSecurityHeadersXFrameOptionsFollowsFrameAncestors(t *testing.T) {
	tests := []struct {
		name           string
		ancestors      []tango.Source
		wantFrameOpt   string
		wantFrameOptOK bool
	}{
		{name: "absent", ancestors: nil, wantFrameOpt: "DENY", wantFrameOptOK: true},

		{name: "none", ancestors: []tango.Source{tango.None()}, wantFrameOpt: "DENY", wantFrameOptOK: true},
		{name: "self", ancestors: []tango.Source{tango.Self()}, wantFrameOpt: "SAMEORIGIN", wantFrameOptOK: true},
		{name: "host omitted", ancestors: []tango.Source{tango.Host("partner.example.com")}, wantFrameOpt: "", wantFrameOptOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := tango.CSP{ImgSrc: []tango.Source{tango.Self()}, FrameAncestors: tt.ancestors}
			response := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)))

			got, ok := response.Header()["X-Frame-Options"]
			if ok != tt.wantFrameOptOK {
				t.Fatalf("X-Frame-Options present = %v, want %v (value %q)", ok, tt.wantFrameOptOK, got)
			}
			if ok && got[0] != tt.wantFrameOpt {
				t.Fatalf("X-Frame-Options = %q, want %q", got[0], tt.wantFrameOpt)
			}
		})
	}
}

func TestSecurityHeadersReferrerPolicyOptionSetsStandardValue(t *testing.T) {
	response := serveThroughSecurityHeaders(t, tango.SecurityHeaders(tango.WithReferrerPolicy("no-referrer")))

	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "no-referrer")
	}
}

func TestSecurityHeadersHandlerOverridesReferrerPolicyForItsResponse(t *testing.T) {
	handler := tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "same-origin")
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := recorder.Header().Get("Referrer-Policy"); got != "same-origin" {
		t.Fatalf("Referrer-Policy = %q, want %q", got, "same-origin")
	}
}

// serveRootRoute serves one GET / route declared with route through a registry
// built from config, and returns the response.
func serveRootRoute(t *testing.T, config tango.Config, route tango.Route) *httptest.ResponseRecorder {
	t.Helper()
	app := tango.NewApp("pages", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{route})
	})
	config.InstalledApps = []tango.App{app}
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	return recorder
}

func TestSecurityHeadersInnerTierReplacesOuterPolicyWithSingleHeader(t *testing.T) {
	outer := tango.SecurityHeaders(tango.WithContentSecurityPolicy(tango.CSP{DefaultSrc: []tango.Source{tango.None()}}))
	inner := tango.SecurityHeaders(tango.WithContentSecurityPolicy(tango.CSP{ImgSrc: []tango.Source{tango.Self()}}))
	view := func(ctx *tango.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}

	response := serveRootRoute(t, tango.Config{Middleware: []tango.Middleware{outer}},
		tango.Path(http.MethodGet, "/", view, tango.Use(inner)))

	values := response.Header().Values("Content-Security-Policy")
	if len(values) != 1 || values[0] != "img-src 'self'" {
		t.Fatalf("Content-Security-Policy values = %q, want exactly [%q]", values, "img-src 'self'")
	}
}

// The inner tier's policy replaces the outer one's header, and the legacy
// X-Frame-Options it cannot express must not survive from the outer tier.
func TestSecurityHeadersInnerHostFrameAncestorsRemovesOuterXFrameOptions(t *testing.T) {
	outer := tango.SecurityHeaders(tango.WithContentSecurityPolicy(tango.CSP{FrameAncestors: []tango.Source{tango.Self()}}))
	inner := tango.SecurityHeaders(tango.WithContentSecurityPolicy(tango.CSP{
		FrameAncestors: []tango.Source{tango.Host("partner.example.com")},
	}))
	view := func(ctx *tango.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}

	response := serveRootRoute(t, tango.Config{Middleware: []tango.Middleware{outer}},
		tango.Path(http.MethodGet, "/", view, tango.Use(inner)))

	if values, ok := response.Header()["X-Frame-Options"]; ok {
		t.Fatalf("X-Frame-Options = %q, want absent: the inner policy allows a host the legacy header cannot name", values)
	}
	if got := response.Header().Get("Content-Security-Policy"); got != "frame-ancestors partner.example.com" {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, "frame-ancestors partner.example.com")
	}
}

func TestSecurityHeadersHSTSIsSentOnlyOverTLS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := tango.SecurityHeaders(tango.WithHSTS(365 * 24 * time.Hour))

	plain := httptest.NewServer(middleware(ok))
	defer plain.Close()
	plainResponse, err := plain.Client().Get(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	plainResponse.Body.Close()
	if values, present := plainResponse.Header["Strict-Transport-Security"]; present {
		t.Fatalf("Strict-Transport-Security sent over plain HTTP: %q", values)
	}

	secure := httptest.NewTLSServer(middleware(ok))
	defer secure.Close()
	secureResponse, err := secure.Client().Get(secure.URL)
	if err != nil {
		t.Fatal(err)
	}
	secureResponse.Body.Close()
	if got := secureResponse.Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("Strict-Transport-Security = %q, want %q", got, "max-age=31536000")
	}
}

func TestSecurityHeadersDoesNotWrapResponseWriter(t *testing.T) {
	handler := tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hijacker := w.(http.Hijacker)
		_, flusher := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hijacker=" + strconv.FormatBool(hijacker) + " flusher=" + strconv.FormatBool(flusher)))
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "hijacker=true flusher=true" {
		t.Fatalf("body = %q, want %q", got, "hijacker=true flusher=true")
	}
}

// serveRootRouteAt serves one GET / route under config and returns the
// response to a GET for path.
func serveRootRouteAt(t *testing.T, config tango.Config, path string) *httptest.ResponseRecorder {
	t.Helper()
	app := tango.NewApp("pages", func(registry *tango.Registry) error {
		return registry.Routes().Include("/", tango.URLs{
			tango.Path(http.MethodGet, "/", func(ctx *tango.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
			}),
		})
	})
	config.InstalledApps = []tango.App{app}
	registry, err := tango.BuildRegistry(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func TestSecurityHeadersUnmatchedRequestCarriesHeadersOnlyUnderScopeAll(t *testing.T) {
	install := tango.SecurityHeaders()

	routesScope := serveRootRouteAt(t, tango.Config{Middleware: []tango.Middleware{install}, MiddlewareScope: tango.MiddlewareScopeRoutes}, "/missing/")
	if routesScope.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", routesScope.Code)
	}
	if got := routesScope.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("MiddlewareScopeRoutes 404 carried Content-Security-Policy %q, want none", got)
	}

	allScope := serveRootRouteAt(t, tango.Config{Middleware: []tango.Middleware{install}, MiddlewareScope: tango.MiddlewareScopeAll}, "/missing/")
	if allScope.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", allScope.Code)
	}
	if got := allScope.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("MiddlewareScopeAll 404 carried no Content-Security-Policy")
	}
}

// zeroRandomSource stands in for crypto/rand, so a nonce is the known value
// "AAAAAAAAAAAAAAAAAAAAAA==" (16 zero bytes in standard base64).
type zeroRandomSource struct{}

func (zeroRandomSource) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestSecurityHeadersNonceInHeaderMatchesNonceHandlerReads(t *testing.T) {
	restore := tango.SetNonceRandomSourceForTest(zeroRandomSource{})
	defer restore()

	const wantNonce = "AAAAAAAAAAAAAAAAAAAAAA=="
	var seen string
	handler := tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = tango.CSPNonce(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen != wantNonce {
		t.Fatalf("CSPNonce = %q, want %q", seen, wantNonce)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "script-src 'self' 'nonce-"+wantNonce+"'") {
		t.Fatalf("Content-Security-Policy = %q does not carry the handler's nonce", got)
	}
}

// countingRandomSource counts the reads made against it.
type countingRandomSource struct{ reads *int }

func (c countingRandomSource) Read(p []byte) (int, error) {
	*c.reads++
	return len(p), nil
}

// failingRandomSource stands in for a crypto/rand that cannot be read.
type failingRandomSource struct{}

func (failingRandomSource) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestSecurityHeadersPolicyWithoutNonceGeneratesNoNonce(t *testing.T) {
	reads := 0
	restore := tango.SetNonceRandomSourceForTest(countingRandomSource{reads: &reads})
	defer restore()

	var seen string
	policy := tango.CSP{DefaultSrc: []tango.Source{tango.Self()}}
	handler := tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = tango.CSPNonce(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if reads != 0 {
		t.Fatalf("nonce source read %d times, want 0", reads)
	}
	if seen != "" {
		t.Fatalf("CSPNonce = %q, want empty", seen)
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("Content-Security-Policy = %q, want %q", got, "default-src 'self'")
	}
}

func TestCSPNonceIsEmptyWithoutMiddleware(t *testing.T) {
	if got := tango.CSPNonce(context.Background()); got != "" {
		t.Fatalf("CSPNonce = %q, want empty", got)
	}
}

func TestSecurityHeadersNonceIsDistinctAndEncodedAcrossRequests(t *testing.T) {
	handler := tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	const requests = 1000
	seen := make(map[string]bool, requests)
	for i := 0; i < requests; i++ {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		nonce := nonceFromPolicy(t, recorder.Header().Get("Content-Security-Policy"))
		if len(nonce) != 24 {
			t.Fatalf("nonce %q has %d characters, want 24", nonce, len(nonce))
		}
		raw, err := base64.StdEncoding.DecodeString(nonce)
		if err != nil || len(raw) != 16 {
			t.Fatalf("nonce %q is not 16 bytes of standard base64: %v", nonce, err)
		}
		if seen[nonce] {
			t.Fatalf("nonce %q repeated within %d requests", nonce, requests)
		}
		seen[nonce] = true
	}
}

// nonceFromPolicy returns the nonce in the script-src of a policy header.
func nonceFromPolicy(t *testing.T, policy string) string {
	t.Helper()
	const prefix = "script-src 'self' 'nonce-"
	start := strings.Index(policy, prefix)
	if start < 0 {
		t.Fatalf("policy %q has no script-src nonce", policy)
	}
	rest := policy[start+len(prefix):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("policy %q has an unterminated nonce", policy)
	}
	return rest[:end]
}

func TestSecurityHeadersFailsClosedWhenNonceCannotBeGenerated(t *testing.T) {
	restore := tango.SetNonceRandomSourceForTest(failingRandomSource{})
	defer restore()

	reached := false
	handler := tango.SecurityHeaders()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if reached {
		t.Fatal("wrapped handler ran after nonce generation failed")
	}
	if got := recorder.Header().Get("Content-Security-Policy"); got != "" {
		t.Fatalf("Content-Security-Policy = %q, want none on the failed response", got)
	}
	if got := recorder.Body.String(); got != "{\"error\":\"internal error\"}\n" {
		t.Fatalf("body = %q, want the generic internal error body", got)
	}
}

func TestSecurityHeadersNestedNonceIsTheInnermostOne(t *testing.T) {
	var seen string
	outer := tango.SecurityHeaders()
	inner := tango.SecurityHeaders()
	view := func(ctx *tango.Context) error {
		seen = tango.CSPNonce(ctx.Context())
		return ctx.JSON(http.StatusOK, map[string]string{"ok": "yes"})
	}

	response := serveRootRoute(t, tango.Config{Middleware: []tango.Middleware{outer}},
		tango.Path(http.MethodGet, "/", view, tango.Use(inner)))

	if seen == "" {
		t.Fatal("handler saw no nonce")
	}
	if got := nonceFromPolicy(t, response.Header().Get("Content-Security-Policy")); got != seen {
		t.Fatalf("header nonce %q differs from the handler's nonce %q", got, seen)
	}
}

func TestNonceAttrRendersThroughHTMLTemplate(t *testing.T) {
	tmpl := template.Must(template.New("page").Funcs(template.FuncMap{"NonceAttr": tango.NonceAttr}).
		Parse(`<script{{NonceAttr .Nonce}}></script>`))

	var withNonce bytes.Buffer
	if err := tmpl.Execute(&withNonce, struct{ Nonce string }{Nonce: "abc+/="}); err != nil {
		t.Fatal(err)
	}
	if got := withNonce.String(); got != `<script nonce="abc+/="></script>` {
		t.Fatalf("with nonce = %q, want %q", got, `<script nonce="abc+/="></script>`)
	}

	var withoutNonce bytes.Buffer
	if err := tmpl.Execute(&withoutNonce, struct{ Nonce string }{}); err != nil {
		t.Fatal(err)
	}
	if got := withoutNonce.String(); got != `<script></script>` {
		t.Fatalf("without nonce = %q, want %q", got, `<script></script>`)
	}
}

func TestViewDataNonceRendersIntoScriptThroughContextHTML(t *testing.T) {
	restore := tango.SetNonceRandomSourceForTest(zeroRandomSource{})
	defer restore()

	tmpl := template.Must(template.New("page").Funcs(template.FuncMap{"NonceAttr": tango.NonceAttr}).
		Parse(`<script{{NonceAttr .Nonce}}>run()</script>`))
	view := func(ctx *tango.Context) error {
		return ctx.HTML(http.StatusOK, tmpl, "page", struct{ Nonce string }{Nonce: tango.CSPNonce(ctx.Context())})
	}

	response := serveRootRoute(t, tango.Config{Middleware: []tango.Middleware{tango.SecurityHeaders()}},
		tango.Path(http.MethodGet, "/", view))

	const wantBody = `<script nonce="AAAAAAAAAAAAAAAAAAAAAA==">run()</script>`
	if got := response.Body.String(); got != wantBody {
		t.Fatalf("body = %q, want %q", got, wantBody)
	}
	if got := response.Header().Get("Content-Security-Policy"); !strings.Contains(got, "'nonce-AAAAAAAAAAAAAAAAAAAAAA=='") {
		t.Fatalf("Content-Security-Policy = %q does not carry the rendered nonce", got)
	}
}

// TestSecurityHeaderOptionsPanicOnInvalidConstructorInput checks the
// constructor-level panics. A panic at construction is the only observable
// outcome for these inputs, so this is the one table test that calls the
// option constructors directly.
func TestSecurityHeaderOptionsPanicOnInvalidConstructorInput(t *testing.T) {
	tests := []struct {
		name  string
		build func()
	}{
		{name: "host with whitespace", build: func() { tango.Host("cdn example.com") }},
		{name: "host with semicolon", build: func() { tango.Host("cdn.example.com;") }},
		{name: "scheme with colon", build: func() { tango.Scheme("https:") }},
		{name: "scheme starting with digit", build: func() { tango.Scheme("1http") }},
		{name: "HSTS under one second", build: func() { tango.WithHSTS(500 * time.Millisecond) }},
		{name: "referrer policy not standard", build: func() { tango.WithReferrerPolicy("STRICT-ORIGIN") }},
		{name: "referrer policy unknown value", build: func() { tango.WithReferrerPolicy("same-origin-ish") }},
		{name: "content security policy with no directives", build: func() { tango.WithContentSecurityPolicy(tango.CSP{}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal("constructor did not panic")
				}
			}()
			tt.build()
		})
	}
}
