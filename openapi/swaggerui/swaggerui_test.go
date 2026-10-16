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
