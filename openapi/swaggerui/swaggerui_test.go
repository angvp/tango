package swaggerui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
	"github.com/angvp/tango/openapi/swaggerui"
)

func site(t *testing.T, opts openapi.Options) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	view := func(ctx *tango.Context) error { return ctx.JSON(http.StatusOK, map[string]string{}) }
	if err := registry.Register(tango.NewApp("shop", func(r *tango.Registry) error {
		return r.Routes().Include("/pets", []tango.Route{tango.Path("GET", "/", view)})
	})); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(openapi.New(opts)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestTheUIIsServedFromTheBinaryWithNoOutsideReferences(t *testing.T) {
	h := site(t, openapi.Options{UI: swaggerui.New(), Path: "/api/openapi.json"})
	pageRec := get(h, "/docs/")
	if pageRec.Code != http.StatusOK || !strings.HasPrefix(pageRec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("page: %d %q", pageRec.Code, pageRec.Header().Get("Content-Type"))
	}
	external := regexp.MustCompile(`(?i)(src|href)\s*=\s*["']?(https?:)?//`)
	if external.MatchString(pageRec.Body.String()) {
		t.Fatalf("the page references another origin:\n%s", pageRec.Body)
	}
	initJS := get(h, "/docs/init.js")
	if !strings.Contains(initJS.Body.String(), `url: "/api/openapi.json"`) || strings.Contains(initJS.Body.String(), "http") {
		t.Fatalf("init.js should point at the local document only:\n%s", initJS.Body)
	}
	for path, kind := range map[string]string{"/docs/swagger-ui.css": "text/css", "/docs/swagger-ui-bundle.js": "text/javascript"} {
		rec := get(h, path)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), kind) || rec.Body.Len() < 100_000 {
			t.Errorf("%s: %d %q, %d bytes", path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
	}
	redirect := get(h, "/docs")
	if redirect.Code != http.StatusSeeOther && redirect.Code != http.StatusFound || redirect.Header().Get("Location") != "/docs/" {
		t.Fatalf("/docs: %d -> %q", redirect.Code, redirect.Header().Get("Location"))
	}
}

func TestTheUIPathCanChangeAndTheUIStaysOutOfTheDocument(t *testing.T) {
	h := site(t, openapi.Options{UI: swaggerui.New(), UIPath: "/reference/"})
	if get(h, "/reference/").Code != http.StatusOK {
		t.Fatal("custom UIPath not served")
	}
	if get(h, "/docs/").Code != http.StatusNotFound {
		t.Fatal("default UIPath still served")
	}
	body := get(h, "/openapi.json").Body.String()
	if strings.Contains(body, "reference") || strings.Contains(body, "swagger") {
		t.Fatalf("the UI's routes are in the document:\n%s", body)
	}
}

func TestNoUIUnlessAskedFor(t *testing.T) {
	h := site(t, openapi.Options{})
	if get(h, "/docs/").Code != http.StatusNotFound {
		t.Fatal("the UI is served without being installed")
	}
}

// The README records the vendored files' hashes; this keeps it honest.
func TestVendoredAssetsMatchTheReadme(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"swagger-ui-bundle.js", "swagger-ui.css"} {
		data, err := os.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if !strings.Contains(string(readme), name+"` | `"+hex.EncodeToString(sum[:])+"`") {
			t.Errorf("README.md does not list the sha256 of %s (%s)", name, hex.EncodeToString(sum[:]))
		}
	}
	if !strings.Contains(string(readme), "**"+swaggerui.Version+"**") {
		t.Errorf("README.md does not name version %s", swaggerui.Version)
	}
}

// policySite serves the UI under an application that installs a
// SecurityHeaders middleware with app as its policy.
func policySite(t *testing.T, ui swaggerui.UI, app tango.CSP) http.Handler {
	t.Helper()
	view := func(ctx *tango.Context) error { return ctx.JSON(http.StatusOK, map[string]string{}) }
	config := tango.Config{
		InstalledApps: []tango.App{
			tango.NewApp("shop", func(r *tango.Registry) error {
				return r.Routes().Include("/pets", []tango.Route{tango.Path("GET", "/", view)})
			}),
			openapi.New(openapi.Options{UI: ui, Path: "/api/openapi.json"}),
		},
		Middleware:      []tango.Middleware{tango.SecurityHeaders(tango.WithContentSecurityPolicy(app))},
		MiddlewareScope: tango.MiddlewareScopeAll,
	}
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
	return handler
}

func swaggerTestPolicy() tango.CSP {
	return tango.CSP{
		DefaultSrc:     []tango.Source{tango.None()},
		ScriptSrc:      []tango.Source{tango.Self()},
		StyleSrc:       []tango.Source{tango.Self()},
		ImgSrc:         []tango.Source{tango.Self()},
		FormAction:     []tango.Source{tango.Self()},
		ObjectSrc:      []tango.Source{tango.None()},
		BaseURI:        []tango.Source{tango.Self()},
		FrameAncestors: []tango.Source{tango.None()},
	}
}

// The page's route relaxes style-src, extends img-src with data:, and extends
// connect-src with 'self' so the page can fetch its own document. Nothing else
// changes.
// The document and the assets keep the application's header.
func TestWithContentSecurityPolicyRelaxesOnlyTheUIPageSourcesItNeeds(t *testing.T) {
	const appHeader = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
	const pageHeader = "default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"
	h := policySite(t, swaggerui.New(swaggerui.WithContentSecurityPolicy(swaggerTestPolicy())), swaggerTestPolicy())

	page := get(h, "/docs/")
	if got := page.Header().Get("Content-Security-Policy"); got != pageHeader {
		t.Errorf("UI page Content-Security-Policy = %q, want %q", got, pageHeader)
	}
	if got := page.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("UI page X-Frame-Options = %q, want DENY", got)
	}
	for _, path := range []string{"/api/openapi.json", "/docs/swagger-ui.css", "/docs/init.js"} {
		if got := get(h, path).Header().Get("Content-Security-Policy"); got != appHeader {
			t.Errorf("%s Content-Security-Policy = %q, want the application header %q", path, got, appHeader)
		}
	}
}

// The page's img-src is the application's effective img-src with data: added.
// Without an img-src directive, images fall back to default-src, so the
// fallback is resolved here rather than replaced with 'self'.
func TestWithContentSecurityPolicyKeepsTheApplicationImageOriginsAndAddsData(t *testing.T) {
	tests := []struct {
		name    string
		app     tango.CSP
		wantImg string
	}{
		{
			name: "host origins are kept and data is appended",
			app: tango.CSP{
				ImgSrc:         []tango.Source{tango.Self(), tango.Host("images.example.com")},
				FrameAncestors: []tango.Source{tango.None()},
			},
			wantImg: "img-src 'self' images.example.com data:; ",
		},
		{
			name:    "absent img-src falls back to default-src",
			app:     tango.CSP{DefaultSrc: []tango.Source{tango.Self()}, FrameAncestors: []tango.Source{tango.None()}},
			wantImg: "img-src 'self' data:; ",
		},
		{
			name:    "default-src none leaves data as the only image source",
			app:     tango.CSP{DefaultSrc: []tango.Source{tango.None()}, FrameAncestors: []tango.Source{tango.None()}},
			wantImg: "img-src data:; ",
		},
		{
			name:    "no img-src and no default-src adds no img-src",
			app:     tango.CSP{FrameAncestors: []tango.Source{tango.None()}},
			wantImg: "",
		},
		{
			name:    "data already present is not repeated",
			app:     tango.CSP{ImgSrc: []tango.Source{tango.Self(), tango.Data()}, FrameAncestors: []tango.Source{tango.None()}},
			wantImg: "img-src 'self' data:; ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := policySite(t, swaggerui.New(swaggerui.WithContentSecurityPolicy(tt.app)), tt.app)
			got := get(h, "/docs/").Header().Get("Content-Security-Policy")
			if tt.wantImg == "" {
				if strings.Contains(got, "img-src") {
					t.Fatalf("UI page Content-Security-Policy = %q, want no img-src directive", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantImg) {
				t.Fatalf("UI page Content-Security-Policy = %q, want img-src part %q", got, tt.wantImg)
			}
		})
	}
}

// The page's connect-src is the application's effective connect-src with
// 'self' added, so the page can fetch its own document. Without a connect-src
// directive the fallback is default-src, and without either no directive is
// added.
func TestWithContentSecurityPolicyKeepsTheApplicationConnectOriginsAndAddsSelf(t *testing.T) {
	tests := []struct {
		name        string
		app         tango.CSP
		wantConnect string
	}{
		{
			name: "host origins are kept and self is appended",
			app: tango.CSP{
				ConnectSrc:     []tango.Source{tango.Host("api.example.com")},
				FrameAncestors: []tango.Source{tango.None()},
			},
			wantConnect: "connect-src api.example.com 'self'; ",
		},
		{
			name:        "absent connect-src falls back to default-src",
			app:         tango.CSP{DefaultSrc: []tango.Source{tango.Host("api.example.com")}, FrameAncestors: []tango.Source{tango.None()}},
			wantConnect: "connect-src api.example.com 'self'; ",
		},
		{
			name:        "default-src none leaves self as the only connect source",
			app:         tango.CSP{DefaultSrc: []tango.Source{tango.None()}, FrameAncestors: []tango.Source{tango.None()}},
			wantConnect: "connect-src 'self'; ",
		},
		{
			name:        "connect-src none is replaced by self",
			app:         tango.CSP{ConnectSrc: []tango.Source{tango.None()}, FrameAncestors: []tango.Source{tango.None()}},
			wantConnect: "connect-src 'self'; ",
		},
		{
			name:        "self already present is not repeated",
			app:         tango.CSP{ConnectSrc: []tango.Source{tango.Self(), tango.Host("api.example.com")}, FrameAncestors: []tango.Source{tango.None()}},
			wantConnect: "connect-src 'self' api.example.com; ",
		},
		{
			name:        "no connect-src and no default-src adds no connect-src",
			app:         tango.CSP{FrameAncestors: []tango.Source{tango.None()}},
			wantConnect: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := policySite(t, swaggerui.New(swaggerui.WithContentSecurityPolicy(tt.app)), tt.app)
			got := get(h, "/docs/").Header().Get("Content-Security-Policy")
			if tt.wantConnect == "" {
				if strings.Contains(got, "connect-src") {
					t.Fatalf("UI page Content-Security-Policy = %q, want no connect-src directive", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantConnect) {
				t.Fatalf("UI page Content-Security-Policy = %q, want connect-src part %q", got, tt.wantConnect)
			}
		})
	}
}

// Without the option the UI adds no header of its own, so an application that
// does not install the middleware sees no change.
func TestTheUIAddsNoPolicyUnlessTheOptionIsGiven(t *testing.T) {
	h := site(t, openapi.Options{UI: swaggerui.New(), Path: "/api/openapi.json"})
	if got := get(h, "/docs/").Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("UI page Content-Security-Policy = %q, want none", got)
	}
}

// The document's body does not depend on the option.
func TestTheDocumentBodyIsTheSameWithTheOption(t *testing.T) {
	plain := site(t, openapi.Options{UI: swaggerui.New(), Path: "/api/openapi.json"})
	relaxed := policySite(t, swaggerui.New(swaggerui.WithContentSecurityPolicy(swaggerTestPolicy())), swaggerTestPolicy())
	if a, b := get(plain, "/api/openapi.json").Body.String(), get(relaxed, "/api/openapi.json").Body.String(); a != b {
		t.Errorf("the document body changed with the option:\nwithout:\n%s\nwith:\n%s", a, b)
	}
}
