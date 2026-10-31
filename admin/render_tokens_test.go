package admin_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// paletteText matches one palette text-colour utility among the utilities of an
// @apply line or a class attribute. A variant prefix (hover:text-ink) does not
// count: the resting colour is what must be named.
var paletteText = regexp.MustCompile(`(?:^|\s)text-(?:ink|ink-muted|ink-faint|accent-fg|success-fg|danger-fg|warning-fg|neutral-fg|danger-solid-fg|sidebar-accent-fg|sidebar-fg|sidebar-strong|sidebar-muted)(?:\s|$)`)

// componentApply matches a single-class rule of the Tailwind source and captures
// the class and its @apply utilities.
var componentApply = regexp.MustCompile(`(?m)^\s*\.([a-z][a-z0-9-]*)\s*\{\s*@apply ([^;}]*);`)

// componentsWithText returns the component classes whose @apply names a
// palette text colour, read from the Tailwind source the stylesheet is built from.
func componentsWithText(t *testing.T) map[string]bool {
	t.Helper()
	named := map[string]bool{}
	for class, apply := range componentRules(t) {
		named[class] = paletteText.MatchString(apply)
	}
	return named
}

// componentRules returns each single-class component's @apply utilities.
func componentRules(t *testing.T) map[string]string {
	t.Helper()
	src, err := os.ReadFile("tailwind/admin.css")
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]string{}
	for _, m := range componentApply.FindAllStringSubmatch(string(src), -1) {
		rules[m[1]] = m[2]
	}
	if len(rules) == 0 {
		t.Fatal("no component rule found in tailwind/admin.css")
	}
	return rules
}

// namesPaletteText reports whether one class attribute carries a palette text
// colour, directly or through a component whose @apply does. Classes are
// compared whole, so stat-value-num is never mistaken for stat-value.
func namesPaletteText(classes string, components map[string]bool) bool {
	if paletteText.MatchString(classes) {
		return true
	}
	for _, c := range strings.Fields(classes) {
		if components[c] {
			return true
		}
	}
	return false
}

// classAttr matches each class="..." attribute in rendered HTML.
var classAttr = regexp.MustCompile(`class="([^"]*)"`)

// rawColour matches a default-palette colour utility (bg-slate-50,
// text-red-600, hover:bg-red-50) inside one class attribute.
var rawColour = regexp.MustCompile(`(?:^|[\s:])(?:bg|text|border|divide|ring|placeholder|from|to|via|fill|stroke)-(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|green|emerald|teal|sky|blue|indigo|violet|purple|pink|rose|white|black)(?:-\d{2,3})?(?:/\d+)?(?:\s|$)`)

// rawColourClasses returns the class attributes that name a raw palette colour.
func rawColourClasses(body string) []string {
	var found []string
	for _, m := range classAttr.FindAllStringSubmatch(body, -1) {
		if rawColour.MatchString(m[1]) {
			found = append(found, m[1])
		}
	}
	return found
}

func TestChangelistDeleteAndLoginCarryNoRawColourClasses(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	id := seedBook(t, sqlDB, "Live Title", true, author)

	pages := map[string]string{
		"changelist": doRequest(t, handler, "GET", flBookPath, nil).Body.String(),
		"delete":     doRequest(t, handler, "GET", fmt.Sprintf("%s%d/delete/", flBookPath, id), nil).Body.String(),
	}
	loginHandler, _ := buildLoginTestHandler(t)
	pages["login"] = doRequest(t, loginHandler, http.MethodGet, "/admin/login/", nil).Body.String()

	for name, body := range pages {
		if raw := rawColourClasses(body); len(raw) > 0 {
			t.Errorf("%s uses raw colour classes: %q", name, raw)
		}
	}
}

func TestChangelistAndDeleteUseTokenClasses(t *testing.T) {
	handler, sqlDB := buildBookAdmin(t, bookFilterOptions())
	author := seedFilterAuthor(t, sqlDB, "Ada")
	id := seedBook(t, sqlDB, "Live Title", true, author)

	list := doRequest(t, handler, "GET", flBookPath, nil).Body.String()
	if !strings.Contains(list, `btn-ghost text-danger-fg hover:bg-danger-bg`) {
		t.Error("changelist lacks the danger ghost button classes")
	}
	components := componentsWithText(t)
	captions := 0
	for _, m := range classAttr.FindAllStringSubmatch(list, -1) {
		if !slices.Contains(strings.Fields(m[1]), "field-caption") {
			continue
		}
		captions++
		if !namesPaletteText(m[1], components) {
			t.Errorf("field caption class %q applies no palette text colour", m[1])
		}
	}
	if captions == 0 {
		t.Error("changelist has no field-caption element")
	}

	del := doRequest(t, handler, "GET", fmt.Sprintf("%s%d/delete/", flBookPath, id), nil).Body.String()
	for _, want := range []string{`text-sm text-ink-muted mb-5`, `font-medium text-ink`} {
		if !strings.Contains(del, want) {
			t.Errorf("delete view lacks %q", want)
		}
	}
}
