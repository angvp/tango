package admin_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

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
	for _, want := range []string{`btn-ghost text-danger-fg hover:bg-danger-bg`, `field-caption`} {
		if !strings.Contains(list, want) {
			t.Errorf("changelist lacks %q", want)
		}
	}

	del := doRequest(t, handler, "GET", fmt.Sprintf("%s%d/delete/", flBookPath, id), nil).Body.String()
	for _, want := range []string{`text-sm text-ink-muted mb-5`, `font-medium text-ink`} {
		if !strings.Contains(del, want) {
			t.Errorf("delete view lacks %q", want)
		}
	}
}
