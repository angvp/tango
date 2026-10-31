package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// adminPolicyHeader is admin.ContentSecurityPolicy as the example sends it on
// /admin/ pages. The literal makes a change to the policy, or to how it is
// rendered, a failing test.
const adminPolicyHeader = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

var nonceInHeader = regexp.MustCompile(`'nonce-([A-Za-z0-9+/=]+)'`)

func renderPage(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec
}

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := rec.Header().Get(name); got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

// scanRendered parses the page and returns each place it breaks the policy.
// An inline script or style is allowed only with the request's nonce, which is
// empty on a page whose policy has none, so such a page allows no inline block.
// No element may carry a style attribute, an event-handler attribute, or a
// src or href that leaves the origin.
func scanRendered(t *testing.T, body, nonce string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
				if a.Key == "style" || strings.HasPrefix(a.Key, "on") {
					problems = append(problems, "<"+n.Data+"> has a "+a.Key+" attribute")
				}
				if (a.Key == "src" || a.Key == "href") && leavesOrigin(a.Val) {
					problems = append(problems, "<"+n.Data+" "+a.Key+"="+a.Val+"> leaves the origin")
				}
			}
			_, hasSrc := attrs["src"]
			inlineAllowed := nonce != "" && attrs["nonce"] == nonce
			if n.Data == "script" && !hasSrc && !inlineAllowed {
				problems = append(problems, "inline <script> without the request nonce")
			}
			if n.Data == "style" && !inlineAllowed {
				problems = append(problems, "inline <style> without the request nonce")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return problems
}

func leavesOrigin(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "//") || strings.Contains(lower, "://") || strings.HasPrefix(lower, "javascript:")
}

func TestTheAdminLoginRunsUnderTheAdminPolicy(t *testing.T) {
	handler, _, _ := newTestServer(t)
	rec := renderPage(t, handler, "/admin/login/")

	assertHeader(t, rec, "Content-Security-Policy", adminPolicyHeader)
	assertHeader(t, rec, "X-Frame-Options", "DENY")
	assertHeader(t, rec, "X-Content-Type-Options", "nosniff")
	assertHeader(t, rec, "Referrer-Policy", "strict-origin-when-cross-origin")
	for _, problem := range scanRendered(t, rec.Body.String(), "") {
		t.Errorf("admin login: %s", problem)
	}
}

func TestTheHomePageUsesThePerRequestNonce(t *testing.T) {
	handler, _, _ := newTestServer(t)
	rec := renderPage(t, handler, "/")

	match := nonceInHeader.FindStringSubmatch(rec.Header().Get("Content-Security-Policy"))
	if match == nil {
		t.Fatalf("Content-Security-Policy carries no nonce: %q", rec.Header().Get("Content-Security-Policy"))
	}
	nonce := match[1]
	assertHeader(t, rec, "Content-Security-Policy",
		"default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; style-src 'self' 'nonce-"+nonce+"'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'")
	assertHeader(t, rec, "X-Frame-Options", "SAMEORIGIN")
	if !strings.Contains(rec.Body.String(), ` nonce="`+nonce+`"`) {
		t.Errorf("the page does not render the header's nonce:\n%s", rec.Body.String())
	}
	for _, problem := range scanRendered(t, rec.Body.String(), nonce) {
		t.Errorf("home page: %s", problem)
	}

	again := nonceInHeader.FindStringSubmatch(renderPage(t, handler, "/").Header().Get("Content-Security-Policy"))
	if again == nil || again[1] == nonce {
		t.Errorf("a second request reuses the nonce %q", nonce)
	}
}

func TestTheAPIKeepsTheGlobalPolicyAndTheAdminPolicyStaysOnItsOwnRoutes(t *testing.T) {
	handler, _, _ := newTestServer(t)
	rec := renderPage(t, handler, "/posts/")

	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "default-src 'self'; script-src 'self' 'nonce-") || strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("/posts/ Content-Security-Policy = %q, want the strict baseline", csp)
	}
}
