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
	{"selection-fg", "selection-bg", 4.5, "selected text"},
	{"sidebar-strong", "sidebar", 4.5, "active sidebar link and brand"},
	{"sidebar-accent-fg", "sidebar-accent", 3, "brand mark glyph"},
	{"ring", "surface", 3, "focus ring on cards"},
	{"ring", "surface-sunken", 3, "focus ring on the page"},
	{"control-line", "surface", 3, "input and checkbox boundary"},
	{"accent-ui", "surface", 3, "checkbox and selected-control fill"},
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

func TestThemeCSSIsGeneratedFromThePalette(t *testing.T) {
	css := themeCSS()
	for _, tok := range palette {
		if !strings.Contains(css, "--"+tok.Name+":") {
			t.Errorf("themeCSS lacks --%s", tok.Name)
		}
	}
	for _, want := range []string{
		":root{color-scheme:light",
		"@media (prefers-color-scheme: dark){:root:not([data-theme]){color-scheme:dark",
		`:root[data-theme="dark"]{color-scheme:dark`,
		`:root[data-theme="light"]{color-scheme:light`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("themeCSS lacks %q", want)
		}
	}
}

func TestTailwindConfigMapsEveryTokenToItsVariable(t *testing.T) {
	cfg := tailwindThemeConfig()
	for _, tok := range palette {
		want := "'" + tok.Name + "':'rgb(var(--" + tok.Name + ") / <alpha-value>)'"
		if !strings.Contains(cfg, want) {
			t.Errorf("tailwind config lacks %s", want)
		}
	}
}

func TestLayoutEmitsTheThemeBeforeAnyPageContent(t *testing.T) {
	var out strings.Builder
	if err := freshLayout().Execute(&out, chrome{}); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"--ink:", "tailwind.config=", "color-scheme:dark"} {
		if !strings.Contains(html, want) {
			t.Errorf("layout lacks %q", want)
		}
	}
	if strings.Index(html, "tailwind.config=") < strings.Index(html, "cdn.tailwindcss.com") {
		t.Error("the Tailwind config must follow the Play CDN script that defines `tailwind`")
	}
}
