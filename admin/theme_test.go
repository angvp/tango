package admin

import (
	"math"
	"strings"
	"testing"
)

// luminance is the WCAG 2.x relative luminance of an sRGB colour.
func luminance(c rgb) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c[0]) + 0.7152*channel(c[1]) + 0.0722*channel(c[2])
}

func contrast(a, b rgb) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func tokenValue(t *testing.T, name string, dark bool) rgb {
	t.Helper()
	for _, tok := range palette {
		if tok.Name == name {
			if dark {
				return tok.Dark
			}
			return tok.Light
		}
	}
	t.Fatalf("palette has no token %q", name)
	return rgb{}
}

// pair is a foreground drawn on a background, with the WCAG minimum the pair
// must meet: 4.5 for normal text, 3 for large text and for the non-text parts
// of a control that are required to identify it. Decorative borders carry no
// promise and are not listed.
type pair struct {
	fg, bg string
	min    float64
	note   string
}

var declaredPairs = []pair{
	{"ink", "surface", 4.5, "body text on cards"},
	{"ink", "surface-sunken", 4.5, "body text on the page"},
	{"ink-muted", "surface", 4.5, "secondary text on cards"},
	{"ink-muted", "surface-sunken", 4.5, "secondary text on the page"},
	{"accent-fg", "accent", 4.5, "primary button label"},
	{"accent-fg", "accent-hover", 4.5, "primary button label on hover"},
	{"danger-solid-fg", "danger-solid", 4.5, "danger button label"},
	{"danger-solid-fg", "danger-solid-hover", 4.5, "danger button label on hover"},
	{"sidebar-fg", "sidebar", 4.5, "sidebar links"},
	{"sidebar-muted", "sidebar", 4.5, "sidebar section labels and footer"},
	{"success-fg", "success-bg", 4.5, "success badge"},
	{"danger-fg", "danger-bg", 4.5, "error alert"},
	{"warning-fg", "warning-bg", 4.5, "warning badge"},
	{"neutral-fg", "neutral-bg", 4.5, "neutral badge"},
	{"ink-faint", "surface", 4.5, "placeholder text"},
	{"ink-faint", "surface-sunken", 4.5, "read-only input text"},
	{"ink-faint", "surface-hover", 4.5, "faint text on a hovered row"},
	{"ink-muted", "surface-hover", 4.5, "table cells and ghost buttons on hover"},
	{"ink", "surface-hover", 4.5, "primary text on hover"},
	{"danger-fg", "surface", 4.5, "field errors and the delete link"},
	{"danger-fg", "danger-bg", 4.5, "delete link on hover"},
	{"selection-fg", "selection-bg", 4.5, "selected text"},
	{"sidebar-strong", "sidebar", 4.5, "brand name and the close control"},
	{"sidebar-fg", "sidebar-pill-hover", 4.5, "sidebar link on hover"},
	{"sidebar-strong", "sidebar-pill-hover", 4.5, "sidebar link text on hover"},
	{"sidebar-fg", "sidebar-pill-current", 4.5, "current sidebar link"},
	{"sidebar-strong", "sidebar-pill-current", 4.5, "current sidebar link text"},
	{"sidebar-muted", "sidebar-pill-hover", 4.5, "sidebar footer text on a hovered pill"},
	{"sidebar-accent-fg", "sidebar-accent", 4.5, "brand mark glyph and pressed theme choice"},
	{"sidebar-accent", "sidebar", 3, "pressed theme choice boundary and brand mark"},
	{"sidebar-accent", "sidebar-pill-current", 3, "current model dot"},
	{"sidebar-dot", "sidebar", 3, "resting model dot"},
	{"sidebar-dot", "sidebar-pill-hover", 3, "model dot on a hovered pill"},
	{"ring", "sidebar", 3, "focus ring on the sidebar"},
	{"ring", "sidebar-pill-hover", 3, "focus ring on a hovered pill"},
	{"ring", "sidebar-pill-current", 3, "focus ring on the current pill"},
	{"ring", "surface", 3, "focus ring on cards"},
	{"ring", "surface-sunken", 3, "focus ring on the page"},
	{"control-line", "surface", 3, "input and checkbox boundary"},
	{"accent-ui", "surface", 3, "checkbox and selected-control fill"},
	{"accent", "surface", 3, "action fill as a control boundary on cards"},
	{"accent", "surface-sunken", 3, "action fill on the page"},
	{"accent-ui", "surface-sunken", 3, "selected-control fill on the page"},
	{"ring", "surface-hover", 3, "focus ring on a hovered row"},
	{"accent-fg", "accent-ui", 3, "label on a selected control"},
	{"success-fg", "surface", 3, "status dot"},
	{"danger-fg", "surface", 3, "status dot"},
	{"warning-fg", "surface", 3, "status dot"},
}

func TestDeclaredTokenPairsMeetWCAGInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		theme := map[bool]string{false: "light", true: "dark"}[dark]
		for _, p := range declaredPairs {
			got := contrast(tokenValue(t, p.fg, dark), tokenValue(t, p.bg, dark))
			if got < p.min {
				t.Errorf("%s: %s on %s (%s) = %.2f:1, want at least %.1f:1", theme, p.fg, p.bg, p.note, got, p.min)
			}
		}
	}
}

// darkPairs are the pairs whose promise holds on dark surfaces only: the
// selection fill has to stand out from the page it is painted over, which the
// pale light-theme selection does not need to do by luminance alone.
var darkPairs = []pair{
	{"selection-bg", "surface", 3, "selection fill against cards"},
	{"selection-bg", "surface-sunken", 3, "selection fill against the page"},
	{"selection-bg", "surface-hover", 3, "selection fill against hovered rows"},
}

func TestDarkSelectionStandsOutFromDarkSurfaces(t *testing.T) {
	for _, p := range darkPairs {
		got := contrast(tokenValue(t, p.fg, true), tokenValue(t, p.bg, true))
		if got < p.min {
			t.Errorf("dark: %s on %s (%s) = %.2f:1, want at least %.1f:1", p.fg, p.bg, p.note, got, p.min)
		}
	}
}

func TestEveryTokenHasBothThemes(t *testing.T) {
	seen := map[string]bool{}
	for _, tok := range palette {
		if seen[tok.Name] {
			t.Errorf("token %q is declared twice", tok.Name)
		}
		seen[tok.Name] = true
		if tok.Light == tok.Dark && !tok.SameInBothThemes {
			t.Errorf("token %q has identical light and dark values; mark SameInBothThemes if that is intended", tok.Name)
		}
	}
}

func TestLayoutLoadsTheStylesheetBeforeTheBody(t *testing.T) {
	var out strings.Builder
	if err := freshLayout().Execute(&out, chrome{}); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	link := strings.Index(html, stylesheetLinkTag)
	if link < 0 {
		t.Fatal("layout does not link the admin stylesheet")
	}
	if link > strings.Index(html, "<body>") {
		t.Error("the admin stylesheet must load in the head, before the body")
	}
}

// Brand roles: navy carries the action, focus and selection on light surfaces;
// cyan does the same on dark surfaces with an ink label, and is never text or
// focus on light. Navy is never drawn on a dark surface.
func TestPaletteCarriesTheBrandRoles(t *testing.T) {
	navy, cyan, ink := rgb{1, 59, 138}, rgb{1, 170, 222}, rgb{11, 31, 63}
	want := []struct {
		name  string
		dark  bool
		value rgb
	}{
		{"accent", false, navy},
		{"accent-ui", false, navy},
		{"ring", false, navy},
		{"ink", false, ink},
		{"surface-sunken", false, rgb{247, 249, 252}},
		{"surface-hover", false, rgb{232, 246, 252}},
		{"accent", true, cyan},
		{"accent-ui", true, cyan},
		{"ring", true, cyan},
		{"accent-fg", true, ink},
		{"surface", true, rgb{10, 26, 54}},
		{"surface-sunken", true, rgb{5, 15, 34}},
		{"sidebar", false, rgb{232, 246, 252}},
		{"sidebar", true, rgb{10, 26, 54}},
		{"sidebar-strong", false, ink},
		{"sidebar-accent", false, navy},
		{"sidebar-accent", true, cyan},
		{"sidebar-accent-fg", true, ink},
		{"sidebar-pill-current", false, rgb{200, 220, 238}},
		{"sidebar-pill-current", true, rgb{8, 52, 84}},
	}
	for _, w := range want {
		if got := tokenValue(t, w.name, w.dark); got != w.value {
			t.Errorf("dark=%v token %s = %v, want %v", w.dark, w.name, got, w.value)
		}
	}
	for _, name := range []string{"accent", "accent-hover", "accent-ui", "ring", "selection-bg", "selection-fg", "ink", "ink-muted", "ink-faint", "surface", "surface-sunken", "surface-hover", "sidebar", "sidebar-fg", "sidebar-strong", "sidebar-muted", "sidebar-dot", "sidebar-accent", "sidebar-accent-fg", "sidebar-pill-hover", "sidebar-pill-current"} {
		if tokenValue(t, name, false) == cyan {
			t.Errorf("light token %s is cyan; cyan is a decorative fill on light surfaces", name)
		}
	}
	for _, tok := range palette {
		if tok.Dark == navy {
			t.Errorf("dark token %s is navy; navy is never used on dark surfaces", tok.Name)
		}
	}
}

// The sidebar is a light surface in light and a dark-surface token in dark:
// no dark-ink sidebar survives in either theme.
func TestSidebarIsLightInLightAndDarkSurfaceInDark(t *testing.T) {
	if got := luminance(tokenValue(t, "sidebar", false)); got < 0.85 {
		t.Errorf("light sidebar luminance = %.2f, want a light ground (at least 0.85)", got)
	}
	if got := luminance(tokenValue(t, "sidebar", true)); got > 0.05 {
		t.Errorf("dark sidebar luminance = %.2f, want a dark surface (at most 0.05)", got)
	}
}
