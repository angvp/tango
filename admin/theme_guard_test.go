package admin_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// rawColourExceptions are the only raw palette classes an admin page may
// carry. Each one is a surface that must not follow the theme.
var rawColourExceptions = map[string]string{
	// The Markdown preview is a sandboxed iframe whose document is rendered
	// outside the theme; its canvas stays white in both themes (ADR 0062).
	"bg-white": "Markdown preview canvas",
}

var tailwindStyleBlock = regexp.MustCompile(`(?s)<style type="text/tailwindcss">(.*?)</style>`)

// themedColourViolations returns every class attribute, and every @apply
// rule in the page's Tailwind style block, that names a raw palette colour
// other than a documented exception.
func themedColourViolations(body string) []string {
	var found []string
	for _, class := range rawColourClasses(withoutExceptions(body)) {
		found = append(found, "class="+class)
	}
	for _, block := range tailwindStyleBlock.FindAllStringSubmatch(body, -1) {
		css := strings.NewReplacer(";", " ", "{", " ", "}", " ").Replace(block[1])
		if rawColour.MatchString(css) {
			for _, word := range strings.Fields(css) {
				if rawColour.MatchString(" " + word + " ") {
					found = append(found, "css "+word)
				}
			}
		}
	}
	return found
}

func withoutExceptions(body string) string {
	for class := range rawColourExceptions {
		body = strings.ReplaceAll(body, " "+class+`"`, `"`)
		body = strings.ReplaceAll(body, " "+class+" ", " ")
	}
	return body
}

func TestRawColourGuardCatchesAClassAndACSSRule(t *testing.T) {
	page := `<div class="card bg-slate-50 p-2"></div><style type="text/tailwindcss">.x { @apply text-red-600 p-2; }</style>`
	got := themedColourViolations(page)
	if len(got) != 2 {
		t.Fatalf("violations = %q, want one class and one css rule", got)
	}
	if ok := themedColourViolations(`<iframe class="border-line-strong bg-white"></iframe>`); len(ok) != 0 {
		t.Errorf("documented exception flagged: %q", ok)
	}
}

func TestRenderedAdminPagesCarryNoRawColourClasses(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	emptyList := doRequest(t, handler, http.MethodGet, flBookPath, nil).Body.String()

	author := seedFilterAuthor(t, sqlDB, "Ada")
	id := seedBook(t, sqlDB, "Live Title", true, author)

	productHandler, productDB := buildProductAdmin(t)
	productID := seedProduct(t, productDB, "Original", 5)

	pages := map[string]string{
		"empty list":  emptyList,
		"list":        doRequest(t, handler, http.MethodGet, flBookPath, nil).Body.String(),
		"create form": doRequest(t, handler, http.MethodGet, flBookPath+"new/", nil).Body.String(),
		"edit form":   doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", flBookPath, id), nil).Body.String(),
		"form error":  doRequest(t, productHandler, http.MethodPost, fmt.Sprintf("%s%d/", crudBasePath, productID), url.Values{"Name": {"x"}, "Price": {"abc"}}).Body.String(),
		"delete":      doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/delete/", flBookPath, id), nil).Body.String(),
		"admin index": getIndex(t, buildDashboardHandler(t)).Body.String(),
		"dashboard":   markupBody(t),
	}
	loginHandler, _ := buildLoginTestHandler(t)
	pages["login"] = doRequest(t, loginHandler, http.MethodGet, "/admin/login/", nil).Body.String()

	for name, body := range pages {
		if len(body) == 0 || !strings.Contains(body, "<html") {
			t.Errorf("%s did not render a page", name)
			continue
		}
		if bad := themedColourViolations(body); len(bad) > 0 {
			t.Errorf("%s names raw palette colours: %q", name, bad)
		}
	}
}
