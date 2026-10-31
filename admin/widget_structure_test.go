package admin_test

import (
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// widgetFieldGroups returns the field group that holds each element a built-in
// widget marks with data-tango-widget. The label, the tabs and the control all
// sit inside that group, so it is the whole of a widget's markup.
func widgetFieldGroups(t *testing.T, body string) []*html.Node {
	t.Helper()
	var groups []*html.Node
	walk(parseShell(t, body), func(n *html.Node) {
		if _, ok := attrOf(n, "data-tango-widget"); !ok {
			return
		}
		for p := n.Parent; p != nil; p = p.Parent {
			if hasClass(p, "field-group") {
				groups = append(groups, p)
				return
			}
		}
		t.Fatalf("widget %q is outside any field group", n.Data)
	})
	if len(groups) == 0 {
		t.Fatalf("no data-tango-widget element in:\n%s", body)
	}
	return groups
}

// hasClass reports whether the element's own start tag carries class, using
// the same token matcher as the component class tests.
func hasClass(n *html.Node, class string) bool {
	var start strings.Builder
	if err := html.Render(&start, &html.Node{Type: html.ElementNode, Data: n.Data, Attr: n.Attr}); err != nil {
		return false
	}
	tag := start.String()
	return hasClassToken(tag[:strings.Index(tag, ">")+1], class)
}

// assertWidgetGroupIsStructural checks the markup a strict policy needs and the
// classes both widgets share with the rest of the form.
func assertWidgetGroupIsStructural(t *testing.T, group *html.Node) {
	t.Helper()
	var label, control bool
	walk(group, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if _, ok := attrOf(n, "style"); ok {
			t.Errorf("widget markup has a style attribute on <%s>", n.Data)
		}
		for _, a := range n.Attr {
			if strings.HasPrefix(a.Key, "on") {
				t.Errorf("widget markup has an inline handler %q on <%s>", a.Key, n.Data)
			}
		}
		switch n.Data {
		case "style":
			t.Errorf("widget markup has a <style> element")
		case "script":
			if _, ok := attrOf(n, "src"); !ok {
				t.Errorf("widget markup has an inline <script>")
			}
		case "label":
			label = label || hasClass(n, "field-label")
		case "textarea", "input":
			control = control || hasClass(n, "input")
		}
		if role, _ := attrOf(n, "role"); role == "tab" && !hasClass(n, "widget-tab") {
			id, _ := attrOf(n, "id")
			t.Errorf("tab %q is not the shared widget-tab class", id)
		}
	})
	if !label {
		t.Errorf("the widget's label is not the shared field-label class")
	}
	if !control {
		t.Errorf("the widget's control is not the shared input class")
	}
}

func TestWidgetFieldGroupsAreStructuralAtTheHTTPSeam(t *testing.T) {
	jsonHandler, _ := buildJSONNoteAdmin(t)
	pages := map[string]string{
		"json":             doRequest(t, jsonHandler, http.MethodGet, noteBasePath+"new/", nil).Body.String(),
		"markdown preview": doRequest(t, markdownPreviewHandler(t, neverCalled(t)), http.MethodGet, noteBasePath+"new/", nil).Body.String(),
	}
	for name, page := range pages {
		groups := widgetFieldGroups(t, page)
		for _, group := range groups {
			assertWidgetGroupIsStructural(t, group)
		}
		if len(groups) != 1 {
			t.Errorf("%s page has %d widget groups, want 1", name, len(groups))
		}
	}
}
