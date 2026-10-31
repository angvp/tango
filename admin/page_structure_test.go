package admin_test

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// headingSkips reports each heading that sits more than one level below the
// heading before it, and a first heading that is not h1.
func headingSkips(levels []int) []string {
	var skips []string
	prev := 0
	for i, level := range levels {
		if i == 0 && level != 1 {
			skips = append(skips, "first heading is not h1")
		}
		if prev != 0 && level > prev+1 {
			skips = append(skips, fmt.Sprintf("h%d followed by h%d", prev, level))
		}
		prev = level
	}
	return skips
}

// family is the page a shell fixture renders: the 422 variant of a form is
// the same page with errors, so it shares its title.
func family(name string) string {
	if before, _, found := strings.Cut(name, "-"); found {
		return before
	}
	return name
}

func headingLevels(doc *html.Node) []int {
	var levels []int
	walk(doc, func(n *html.Node) {
		if len(n.Data) == 2 && n.Data[0] == 'h' && n.Data[1] >= '1' && n.Data[1] <= '6' {
			levels = append(levels, int(n.Data[1]-'0'))
		}
	})
	return levels
}

func TestEveryAdminPageHasItsDocumentStructure(t *testing.T) {
	titles := map[string]string{}
	for name, body := range shellPages(t) {
		doc := parseShell(t, body)

		root := findAll(doc, func(n *html.Node) bool { return n.Data == "html" })
		if len(root) != 1 || attrValue(root[0], "lang") == "" {
			t.Errorf("%s: <html> has no lang attribute", name)
		}

		title := findAll(doc, func(n *html.Node) bool { return n.Data == "title" })
		if len(title) != 1 || textOf(title[0]) == "" {
			t.Errorf("%s: <title> is missing or empty", name)
		} else if previous, dup := titles[textOf(title[0])]; dup && previous != family(name) {
			t.Errorf("%s: <title> %q is also the title of %s", name, textOf(title[0]), previous)
		} else {
			titles[textOf(title[0])] = family(name)
		}

		if n := findAll(doc, func(n *html.Node) bool { return n.Data == "main" }); len(n) != 1 {
			t.Errorf("%s: %d <main> elements, want 1", name, len(n))
		}

		if n := findAll(doc, func(n *html.Node) bool { return attrValue(n, "role") == "status" }); len(n) != 1 {
			t.Errorf("%s: %d role=\"status\" regions, want 1", name, len(n))
		}

		navs := findAll(doc, func(n *html.Node) bool { return n.Data == "nav" })
		for _, nav := range navs {
			if attrValue(nav, "aria-label") == "" {
				t.Errorf("%s: a <nav> has no aria-label", name)
			}
		}

		for _, skip := range headingSkips(headingLevels(doc)) {
			t.Errorf("%s: heading order: %s", name, skip)
		}
	}
}
