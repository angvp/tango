package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

var adminStylesheetLink = regexp.MustCompile(`<link rel="stylesheet" href="(/admin/_assets/admin\.[0-9a-f]{16}\.css)">`)

// servedAdminStylesheet loads the login page, follows its stylesheet link and
// returns the URL and the asset's response.
func servedAdminStylesheet(t *testing.T) (string, *httptest.ResponseRecorder) {
	t.Helper()
	handler := buildAdminHandler(t)
	page := doRequest(t, handler, http.MethodGet, "/admin/login/", nil).Body.String()
	match := adminStylesheetLink.FindStringSubmatch(page)
	if match == nil {
		t.Fatalf("login page links no hashed admin stylesheet:\n%s", page)
	}
	return match[1], doRequest(t, handler, http.MethodGet, match[1], nil)
}

func TestAdminLayoutLinksItsStylesheetAndNoThirdPartyOrInlineStyle(t *testing.T) {
	handler := buildAdminHandler(t)
	page := doRequest(t, handler, http.MethodGet, "/admin/login/", nil).Body.String()

	if !adminStylesheetLink.MatchString(page) {
		t.Fatalf("login page links no hashed admin stylesheet:\n%s", page)
	}
	for _, banned := range []string{"cdn.tailwindcss.com", "tailwind.config", "<style"} {
		if strings.Contains(page, banned) {
			t.Errorf("layout still contains %q", banned)
		}
	}
}

func TestAdminStylesheetIsServedAsCacheableCSS(t *testing.T) {
	link, response := servedAdminStylesheet(t)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", link, response.Code)
	}
	wantHeaders := map[string]string{
		"Content-Type":           "text/css; charset=utf-8",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "public, max-age=31536000, immutable",
	}
	for name, want := range wantHeaders {
		if got := response.Header().Get(name); got != want {
			t.Errorf("%s: %s = %q, want %q", link, name, got, want)
		}
	}
}

// ruleBody returns the declarations of the first rule whose selector matches
// selector, or "" when none does.
func ruleBody(css, selector string) string {
	match := regexp.MustCompile(selector + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
	if match == nil {
		return ""
	}
	return match[1]
}

var (
	colorSchemeDark  = regexp.MustCompile(`color-scheme:\s*dark`)
	colorSchemeLight = regexp.MustCompile(`color-scheme:\s*light`)
)

// Each theme block must declare every token with its palette value: the light
// block for the default and for an explicit light choice, the dark block for
// the system preference and for an explicit dark choice (ADR 0062).
func TestAdminStylesheetDeclaresEveryTokenInEachThemeBlock(t *testing.T) {
	_, response := servedAdminStylesheet(t)
	css := response.Body.String()

	blocks := []struct {
		name, selector string
		dark           bool
	}{
		{"light default", `:root`, false},
		{"system dark", `@media \(prefers-color-scheme: dark\)\s*\{\s*:root:not\(\[data-theme\]\)`, true},
		{"explicit dark", `:root\[data-theme="dark"\]`, true},
		{"explicit light", `:root\[data-theme="light"\]`, false},
	}
	for _, block := range blocks {
		body := ruleBody(css, block.selector)
		if body == "" {
			t.Errorf("stylesheet has no %s block", block.name)
			continue
		}
		if block.dark && !colorSchemeDark.MatchString(body) {
			t.Errorf("%s block does not set color-scheme: dark", block.name)
		}
		if !block.dark && !colorSchemeLight.MatchString(body) {
			t.Errorf("%s block does not set color-scheme: light", block.name)
		}
		for _, tok := range admin.TokenPaletteForTest {
			value := tok.Light
			if block.dark {
				value = tok.Dark
			}
			want := regexp.MustCompile(fmt.Sprintf(`--%s:\s*%d %d %d\s*;`, regexp.QuoteMeta(tok.Name), value[0], value[1], value[2]))
			if !want.MatchString(body) {
				t.Errorf("%s block lacks token --%s with value %d %d %d", block.name, tok.Name, value[0], value[1], value[2])
			}
		}
	}
}

// rawPaletteSelector matches a utility class that names a default-palette
// colour in the served stylesheet.
var rawPaletteSelector = regexp.MustCompile(`\.(?:bg|text|border|ring|divide|placeholder|from|to|via|fill|stroke|outline|shadow|accent|caret)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d+\b`)

func TestAdminStylesheetNamesNoRawPaletteColour(t *testing.T) {
	_, response := servedAdminStylesheet(t)
	if found := rawPaletteSelector.FindAllString(response.Body.String(), -1); len(found) > 0 {
		t.Errorf("stylesheet names raw palette colours: %q", found)
	}
}

// Each token's text, background and border utility must read that token's own
// custom property, so the Tailwind colour map cannot point a class at the wrong
// variable.
func TestAdminStylesheetMapsEveryTokenUtilityToItsVariable(t *testing.T) {
	_, response := servedAdminStylesheet(t)
	css := response.Body.String()
	for _, tok := range admin.TokenPaletteForTest {
		for _, prefix := range []string{"text", "bg", "border"} {
			selector := regexp.QuoteMeta("." + prefix + "-" + tok.Name)
			body := ruleBody(css, selector)
			if want := "var(--" + tok.Name + ")"; !strings.Contains(body, want) {
				t.Errorf("the %s utility for token %s does not read %s; rule body %q", prefix, tok.Name, want, body)
			}
		}
	}
}

// Host widgets write the token classes the admin guide documents, and the
// build cannot see their markup, so every token has its text, background and
// border utility in the served stylesheet.
func TestAdminStylesheetGeneratesTheTokenUtilitiesWidgetsUse(t *testing.T) {
	_, response := servedAdminStylesheet(t)
	css := response.Body.String()
	for _, tok := range admin.TokenPaletteForTest {
		for _, prefix := range []string{"text", "bg", "border"} {
			if selector := "." + prefix + "-" + tok.Name + "{"; !strings.Contains(css, selector) {
				t.Errorf("stylesheet lacks the %s utility for token %s", prefix, tok.Name)
			}
		}
	}
}
