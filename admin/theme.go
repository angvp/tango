package admin

import (
	"fmt"
	"strings"
)

// rgb is one sRGB colour as 0-255 channels.
type rgb [3]uint8

// themeToken is one semantic colour of the admin: the name Tailwind classes
// use (bg-surface, text-ink-muted), and the value it takes in each theme.
// SameInBothThemes marks the tokens whose light and dark values are equal on
// purpose, such as white text on a coloured button.
type themeToken struct {
	Name             string
	Light, Dark      rgb
	SameInBothThemes bool
}

// palette is the single source of the admin's colours. The CSS custom
// properties, the Tailwind colour map and the contrast tests are all
// generated from it, so a value changes in one place.
var palette = []themeToken{
	// Text.
	{Name: "ink", Light: rgb{15, 23, 42}, Dark: rgb{241, 245, 249}},
	{Name: "ink-muted", Light: rgb{71, 85, 105}, Dark: rgb{148, 163, 184}},
	{Name: "ink-faint", Light: rgb{100, 116, 139}, Dark: rgb{130, 145, 166}},

	// Surfaces.
	{Name: "surface", Light: rgb{255, 255, 255}, Dark: rgb{30, 41, 59}},
	{Name: "surface-sunken", Light: rgb{248, 250, 252}, Dark: rgb{15, 23, 42}},
	{Name: "surface-hover", Light: rgb{241, 245, 249}, Dark: rgb{51, 65, 85}},

	// Lines: line and line-subtle are decorative dividers; control-line is
	// the boundary of an input, which must stay visible.
	{Name: "line", Light: rgb{226, 232, 240}, Dark: rgb{51, 65, 85}},
	{Name: "line-subtle", Light: rgb{241, 245, 249}, Dark: rgb{41, 53, 72}},
	{Name: "line-strong", Light: rgb{203, 213, 225}, Dark: rgb{71, 85, 105}},
	{Name: "control-line", Light: rgb{116, 129, 148}, Dark: rgb{112, 128, 150}},

	// Accent: the primary button, selected pagination, checkboxes, focus.
	{Name: "accent", Light: rgb{79, 70, 229}, Dark: rgb{79, 70, 229}, SameInBothThemes: true},
	{Name: "accent-hover", Light: rgb{67, 56, 202}, Dark: rgb{67, 56, 202}, SameInBothThemes: true},
	{Name: "accent-fg", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},
	{Name: "accent-ui", Light: rgb{79, 70, 229}, Dark: rgb{129, 140, 248}},
	{Name: "ring", Light: rgb{99, 102, 241}, Dark: rgb{129, 140, 248}},
	{Name: "selection-bg", Light: rgb{199, 210, 254}, Dark: rgb{55, 48, 163}},
	{Name: "selection-fg", Light: rgb{30, 27, 75}, Dark: rgb{224, 231, 255}},

	// Destructive button.
	{Name: "danger-solid", Light: rgb{220, 38, 38}, Dark: rgb{220, 38, 38}, SameInBothThemes: true},
	{Name: "danger-solid-hover", Light: rgb{185, 28, 28}, Dark: rgb{185, 28, 28}, SameInBothThemes: true},
	{Name: "danger-solid-fg", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},

	// Sidebar: slate-950 in light; a shade deeper than the content in dark.
	{Name: "sidebar", Light: rgb{2, 6, 23}, Dark: rgb{8, 12, 26}},
	{Name: "sidebar-fg", Light: rgb{203, 213, 225}, Dark: rgb{203, 213, 225}, SameInBothThemes: true},
	{Name: "sidebar-strong", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},
	{Name: "sidebar-muted", Light: rgb{148, 163, 184}, Dark: rgb{148, 163, 184}, SameInBothThemes: true},
	{Name: "sidebar-dot", Light: rgb{71, 85, 105}, Dark: rgb{71, 85, 105}, SameInBothThemes: true},
	{Name: "sidebar-accent", Light: rgb{99, 102, 241}, Dark: rgb{99, 102, 241}, SameInBothThemes: true},
	{Name: "sidebar-accent-fg", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},

	// Status family: background, text and border of each.
	{Name: "success-bg", Light: rgb{209, 250, 229}, Dark: rgb{6, 78, 59}},
	{Name: "success-fg", Light: rgb{4, 120, 87}, Dark: rgb{110, 231, 183}},
	{Name: "success-border", Light: rgb{167, 243, 208}, Dark: rgb{4, 120, 87}},
	{Name: "danger-bg", Light: rgb{254, 242, 242}, Dark: rgb{69, 10, 10}},
	{Name: "danger-fg", Light: rgb{185, 28, 28}, Dark: rgb{252, 165, 165}},
	{Name: "danger-border", Light: rgb{254, 202, 202}, Dark: rgb{127, 29, 29}},
	{Name: "warning-bg", Light: rgb{254, 243, 199}, Dark: rgb{69, 26, 3}},
	{Name: "warning-fg", Light: rgb{146, 64, 14}, Dark: rgb{252, 211, 77}},
	{Name: "warning-border", Light: rgb{253, 230, 138}, Dark: rgb{146, 64, 14}},
	{Name: "neutral-bg", Light: rgb{241, 245, 249}, Dark: rgb{51, 65, 85}},
	{Name: "neutral-fg", Light: rgb{71, 85, 105}, Dark: rgb{203, 213, 225}},
	{Name: "neutral-border", Light: rgb{226, 232, 240}, Dark: rgb{71, 85, 105}},
}

func (c rgb) channels() string {
	return fmt.Sprintf("%d %d %d", c[0], c[1], c[2])
}

// declarations renders the tokens as custom-property declarations for one
// theme.
func declarations(dark bool) string {
	var b strings.Builder
	for _, tok := range palette {
		value := tok.Light
		if dark {
			value = tok.Dark
		}
		fmt.Fprintf(&b, "--%s:%s;", tok.Name, value.channels())
	}
	return b.String()
}

// themeCSS returns the stylesheet that defines every token. Light is the
// default; the dark values apply when the browser prefers dark and no theme
// was chosen, or when the chosen theme (data-theme on <html>) is dark.
func themeCSS() string {
	dark := declarations(true)
	return ":root{color-scheme:light;" + declarations(false) + "}" +
		"@media (prefers-color-scheme: dark){:root:not([data-theme]){color-scheme:dark;" + dark + "}}" +
		`:root[data-theme="dark"]{color-scheme:dark;` + dark + "}" +
		`:root[data-theme="light"]{color-scheme:light;` + declarations(false) + "}"
}

// tailwindThemeConfig returns the script body that maps each token to a
// Tailwind colour, so bg-surface and text-ink/60 read the custom properties.
func tailwindThemeConfig() string {
	var b strings.Builder
	b.WriteString("tailwind.config={theme:{extend:{colors:{")
	for i, tok := range palette {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "'%s':'rgb(var(--%s) / <alpha-value>)'", tok.Name, tok.Name)
	}
	b.WriteString("}}}}")
	return b.String()
}
