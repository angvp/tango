package admin_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseShell(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func attrOf(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func findAll(doc *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	walk(doc, func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
		}
	})
	return found
}

func textOf(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

func shellPages(t *testing.T) map[string]string {
	t.Helper()
	handler, sqlDB := buildProductAdminWithOptions(t, bulkProductOptions())
	id := idString(seedProduct(t, sqlDB, "Widget", 3))
	pages := map[string]string{
		"list":   doRequest(t, handler, http.MethodGet, crudBasePath, nil).Body.String(),
		"new":    doRequest(t, handler, http.MethodGet, crudBasePath+"new/", nil).Body.String(),
		"edit":   doRequest(t, handler, http.MethodGet, crudBasePath+id+"/", nil).Body.String(),
		"delete": doRequest(t, handler, http.MethodGet, crudBasePath+id+"/delete/", nil).Body.String(),
		"bulk":   doRequest(t, handler, http.MethodPost, crudBasePath+"delete-selected/", url.Values{}).Body.String(),
		"login":  doRequest(t, buildAdminHandler(t), http.MethodGet, "/admin/login/", nil).Body.String(),
	}
	for name, body := range pages {
		if !strings.Contains(body, "<main") {
			t.Fatalf("%s page is not an admin page:\n%s", name, body)
		}
	}
	return pages
}

func TestEveryAdminPageStartsWithASkipLinkToTheMainLandmark(t *testing.T) {
	for name, body := range shellPages(t) {
		doc := parseShell(t, body)
		focusable := findAll(doc, func(n *html.Node) bool {
			_, href := attrOf(n, "href")
			return (n.Data == "a" && href) || n.Data == "button" || n.Data == "input"
		})
		if len(focusable) == 0 {
			t.Fatalf("%s: no focusable element", name)
		}
		first := focusable[0]
		if href, _ := attrOf(first, "href"); first.Data != "a" || href != "#main" {
			t.Errorf("%s: first focusable is <%s href=%q>, want the skip link to #main", name, first.Data, href)
		}
		mains := findAll(doc, func(n *html.Node) bool { return n.Data == "main" })
		if len(mains) != 1 {
			t.Fatalf("%s: %d <main> elements, want 1", name, len(mains))
		}
		if id, _ := attrOf(mains[0], "id"); id != "main" {
			t.Errorf("%s: <main id=%q>, want main", name, id)
		}
	}
}

func TestBreadcrumbsLinkTheModelAndMarkTheCurrentItem(t *testing.T) {
	pages := shellPages(t)
	tests := []struct {
		page, current string
	}{
		{"edit", "Widget"},
		{"delete", "Widget"},
		{"bulk", ""},
	}
	for _, tt := range tests {
		doc := parseShell(t, pages[tt.page])
		navs := findAll(doc, func(n *html.Node) bool {
			label, _ := attrOf(n, "aria-label")
			return n.Data == "nav" && label == "Breadcrumb"
		})
		if len(navs) != 1 {
			t.Fatalf("%s: %d breadcrumb navs, want 1", tt.page, len(navs))
		}
		links := findAll(navs[0], func(n *html.Node) bool { return n.Data == "a" })
		if len(links) != 1 {
			t.Fatalf("%s: %d breadcrumb links, want 1 (the model)", tt.page, len(links))
		}
		if href, _ := attrOf(links[0], "href"); href != crudBasePath {
			t.Errorf("%s: model crumb href = %q, want %q", tt.page, href, crudBasePath)
		}
		if tt.current == "" {
			continue
		}
		current := findAll(navs[0], func(n *html.Node) bool {
			v, ok := attrOf(n, "aria-current")
			return ok && v == "page"
		})
		if len(current) != 1 || current[0].Data == "a" || textOf(current[0]) != tt.current {
			t.Errorf("%s: current crumb = %v, want text %q that is not a link", tt.page, current, tt.current)
		}
	}
	list := parseShell(t, pages["list"])
	if n := findAll(list, func(n *html.Node) bool { return n.Data == "nav" && attrValue(n, "aria-label") == "Breadcrumb" }); len(n) != 0 {
		t.Errorf("list page renders breadcrumbs")
	}
}

func attrValue(n *html.Node, key string) string {
	v, _ := attrOf(n, key)
	return v
}

func TestPhoneDrawerHasACloseButtonAndAnAnnouncedMenuButton(t *testing.T) {
	for name, body := range shellPages(t) {
		doc := parseShell(t, body)
		sidebar := findAll(doc, func(n *html.Node) bool { return attrValue(n, "id") == "sidebar" })
		if len(sidebar) != 1 {
			t.Fatalf("%s: %d #sidebar, want 1", name, len(sidebar))
		}
		closers := findAll(sidebar[0], func(n *html.Node) bool {
			_, ok := attrOf(n, "data-admin-nav-close")
			return n.Data == "button" && ok
		})
		if len(closers) != 1 || textOf(closers[0]) != "Close menu" {
			t.Errorf("%s: drawer close buttons = %d, want one labelled Close menu", name, len(closers))
		}
		for _, opener := range findAll(doc, func(n *html.Node) bool { _, ok := attrOf(n, "data-admin-nav-open"); return ok }) {
			if attrValue(opener, "aria-expanded") != "false" || attrValue(opener, "aria-controls") != "sidebar" {
				t.Errorf("%s: menu button lacks aria-expanded=false and aria-controls=sidebar", name)
			}
		}
	}
}

func TestSidebarMarksTheCurrentModelAndThemeSwitchStaysAPostForm(t *testing.T) {
	body := shellPages(t)["list"]
	doc := parseShell(t, body)
	current := findAll(doc, func(n *html.Node) bool {
		return n.Data == "a" && attrValue(n, "aria-current") == "page"
	})
	if len(current) != 1 || attrValue(current[0], "href") != crudBasePath {
		t.Errorf("sidebar current links = %d, want the one model link", len(current))
	}
	forms := findAll(doc, func(n *html.Node) bool { return n.Data == "form" && attrValue(n, "action") == "/admin/theme/" })
	if len(forms) != 1 || attrValue(forms[0], "method") != "post" {
		t.Fatalf("theme switch is not a single POST form: %d", len(forms))
	}
	buttons := findAll(forms[0], func(n *html.Node) bool { return n.Data == "button" && attrValue(n, "type") == "submit" })
	if len(buttons) != 3 {
		t.Fatalf("theme buttons = %d, want Light, Dark, System", len(buttons))
	}
	pressed := 0
	for _, b := range buttons {
		if textOf(b) == "" {
			t.Errorf("theme button has no visible text")
		}
		if attrValue(b, "aria-pressed") == "true" {
			pressed++
		}
	}
	if pressed != 1 {
		t.Errorf("pressed theme buttons = %d, want 1", pressed)
	}
}

func TestShellRendersNoInlineScriptStyleOrHandler(t *testing.T) {
	for name, body := range shellPages(t) {
		doc := parseShell(t, body)
		for _, n := range findAll(doc, func(*html.Node) bool { return true }) {
			if n.Data == "style" {
				t.Errorf("%s: inline <style>", name)
			}
			if n.Data == "script" {
				if _, ok := attrOf(n, "src"); !ok || n.FirstChild != nil {
					t.Errorf("%s: inline <script>", name)
				}
			}
			for _, a := range n.Attr {
				if a.Key == "style" || strings.HasPrefix(a.Key, "on") {
					t.Errorf("%s: <%s> carries %s=", name, n.Data, a.Key)
				}
			}
		}
	}
}

func TestBulkDialogBackdropUsesTheScrimToken(t *testing.T) {
	doc := parseShell(t, shellPages(t)["list"])
	dialogs := findAll(doc, func(n *html.Node) bool { _, ok := attrOf(n, "data-bulk-dialog"); return n.Data == "dialog" && ok })
	if len(dialogs) != 1 {
		t.Fatalf("%d bulk dialogs, want 1", len(dialogs))
	}
	classes := strings.Fields(attrValue(dialogs[0], "class"))
	has := func(c string) bool {
		for _, x := range classes {
			if x == c {
				return true
			}
		}
		return false
	}
	if !has("backdrop:bg-scrim/60") || has("backdrop:bg-sidebar/50") {
		t.Errorf("dialog classes = %q, want the scrim backdrop", classes)
	}
}
