package admin_test

import (
	"context"
	"html/template"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

// Repeated utility chains are named components, so a restyle changes one rule
// and the markup carries no copy of it.
func TestDashboardUsesNamedComponentsForRepeatedChains(t *testing.T) {
	body := markupBody(t)
	for _, banned := range []string{"divide-y", "text-hint", "before:inline-block", "before:rounded-full"} {
		if strings.Contains(body, banned) {
			t.Errorf("dashboard markup carries %q instead of a named component", banned)
		}
	}
	for _, want := range []string{`class="row-list"`, `class="hint-text"`, "status-dot"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard markup lacks %s", want)
		}
	}
}

func TestMarkdownWidgetHintsUseTheNamedHintClass(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown(admin.WithMarkdownPreview(func(context.Context, string) (template.HTML, error) { return "", nil }))}})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	if strings.Contains(body, "text-xs text-ink-faint") {
		t.Error("Markdown widget repeats the hint utility chain")
	}
	if got := strings.Count(body, "hint-small"); got != 2 {
		t.Errorf("hint-small count = %d, want the count and the preview status", got)
	}
}

func TestThemeSwitchStylesChoicesWithNamedClasses(t *testing.T) {
	handler, _ := buildProductAdmin(t)
	body := getWithCookies(t, handler, crudBasePath,
		&http.Cookie{Name: "tango_admin_session", Value: testSessionToken},
		&http.Cookie{Name: "tango_admin_theme", Value: "dark"},
	).Body.String()
	want := map[string]string{"light": "theme-option-off", "dark": "theme-option-on", "system": "theme-option-off"}
	for value, class := range want {
		button := regexp.MustCompile(`<button type="submit" name="theme" value="` + value + `"[^>]*>`).FindString(body)
		if !strings.Contains(button, class) {
			t.Errorf("%s button %q lacks %s", value, button, class)
		}
	}
	components := componentsWithText(t)
	for _, class := range []string{"theme-option-on", "theme-option-off"} {
		if !components[class] {
			t.Errorf("%s applies no palette text colour", class)
		}
	}
}

// Cards take their corner radius from one named token, not from an ad-hoc
// radius utility.
func TestCardRadiusIsOneNamedToken(t *testing.T) {
	rules := componentRules(t)
	if !regexp.MustCompile(`(?:^|\s)rounded-card(?:\s|$)`).MatchString(rules["card"]) {
		t.Errorf(".card applies %q, want the named rounded-card radius", rules["card"])
	}
	config, err := os.ReadFile("tailwind/tailwind.config.js")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`borderRadius:\s*\{[^}]*'card':\s*'18px'`).Match(config) {
		t.Error("tailwind config does not define the card radius as 18px")
	}
}
