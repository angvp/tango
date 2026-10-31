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
	{"danger-fg", "surface-sunken", 4.5, "delete link on the page ground"},
	{"ring", "danger-bg", 3, "focus ring on the delete link on hover"},
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
	{"success-fg", "surface", 3, "success badge text on the card"},
	{"danger-fg", "surface", 3, "danger badge text on the card"},
	{"warning-fg", "surface", 3, "warning badge text on the card"},
	{"ink", "header", 4.5, "page title and header text"},
	{"ink-muted", "header", 4.5, "breadcrumbs and header links"},
	{"ink-faint", "header", 4.5, "header meta text"},
	{"accent", "header", 3, "header action boundary"},
	{"ring", "header", 3, "focus ring on the header"},
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

// The header is a solid token, so the ratio below is the colour that is painted.
// In dark it must lift off both the sidebar and the page ground; in light the
// header is the white surface, so the sidebar's pale tint is the only fill
// it has to separate from, and the bottom border carries the rest.
func TestHeaderIsDistinctFromSidebarAndPage(t *testing.T) {
	cases := []struct {
		dark    bool
		against string
		min     float64
	}{
		{true, "sidebar", 1.3},
		{true, "surface-sunken", 1.3},
		{false, "sidebar", 1.05},
	}
	for _, c := range cases {
		theme := map[bool]string{false: "light", true: "dark"}[c.dark]
		got := contrast(tokenValue(t, "header", c.dark), tokenValue(t, c.against, c.dark))
		if got < c.min {
			t.Errorf("%s: header against %s = %.2f:1, want at least %.2f:1", theme, c.against, got, c.min)
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
		{"scrim", false, ink},
		{"scrim", true, rgb{0, 0, 0}},
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
// the sidebar takes a surface ground in each theme, never the old dark ink.
func TestSidebarIsLightInLightAndDarkSurfaceInDark(t *testing.T) {
	if got := luminance(tokenValue(t, "sidebar", false)); got < 0.85 {
		t.Errorf("light sidebar luminance = %.2f, want a light ground (at least 0.85)", got)
	}
	if got := luminance(tokenValue(t, "sidebar", true)); got > 0.05 {
		t.Errorf("dark sidebar luminance = %.2f, want a dark surface (at most 0.05)", got)
	}
}

// scrimAlpha is the opacity the stylesheet applies to the scrim token
// (bg-scrim/60); the smoke reads the computed backdrop colour to keep the two
// equal.
const scrimAlpha = 0.6

func blend(over rgb, alpha float64, under rgb) rgb {
	var out rgb
	for i := range out {
		out[i] = uint8(math.Round(float64(over[i])*alpha + float64(under[i])*(1-alpha)))
	}
	return out
}

// The scrim darkens the page behind the phone drawer and the bulk delete
// dialog in both themes. It is drawn over the page ground (surface-sunken) and
// over cards (surface).
func TestScrimDarkensThePageInBothThemes(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, underlay := range []string{"surface", "surface-sunken"} {
			under := tokenValue(t, underlay, dark)
			dimmed := blend(tokenValue(t, "scrim", dark), scrimAlpha, under)
			if got, ceiling := luminance(dimmed), luminance(under)/2; got > ceiling {
				t.Errorf("dark=%v scrim over %s: luminance %.4f, want at most %.4f (half the page's)", dark, underlay, got, ceiling)
			}
		}
	}
}

// The open drawer has to read against the dimmed page. In light its ground is
// pale against a dimmed page. In dark the drawer and the page are both near
// black, so no scrim separates them by fill: the drawer's edge does.
func TestOpenDrawerIsDistinctFromTheDimmedPage(t *testing.T) {
	light := blend(tokenValue(t, "scrim", false), scrimAlpha, tokenValue(t, "surface-sunken", false))
	if got := contrast(tokenValue(t, "sidebar", false), light); got < 3 {
		t.Errorf("light: drawer against the dimmed page = %.2f:1, want at least 3:1", got)
	}
	dark := blend(tokenValue(t, "scrim", true), scrimAlpha, tokenValue(t, "surface-sunken", true))
	if got := contrast(tokenValue(t, "line-strong", true), dark); got < 1.8 {
		t.Errorf("dark: drawer edge against the dimmed page = %.2f:1, want at least 1.8:1", got)
	}
}
