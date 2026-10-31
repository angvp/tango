package admin

// The palette is the admin's colour table. Nothing in the binary reads it, so it
// lives with the tests: the contrast tests read it directly, and the stylesheet
// tests read it through TokenPaletteForTest and compare it with the custom
// properties in the served admin/assets/admin.css. The colour map is in
// admin/tailwind/tailwind.config.js, and scripts/admin-css.sh --check fails when
// the generated stylesheet drifts from it.

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

var palette = []themeToken{
	// Text.
	{Name: "ink", Light: rgb{11, 31, 63}, Dark: rgb{241, 245, 249}},
	{Name: "ink-muted", Light: rgb{52, 70, 102}, Dark: rgb{148, 163, 184}},
	{Name: "ink-faint", Light: rgb{84, 100, 126}, Dark: rgb{143, 158, 180}},

	// Surfaces.
	{Name: "surface", Light: rgb{255, 255, 255}, Dark: rgb{10, 26, 54}},
	{Name: "surface-sunken", Light: rgb{247, 249, 252}, Dark: rgb{5, 15, 34}},
	{Name: "surface-hover", Light: rgb{232, 246, 252}, Dark: rgb{20, 40, 76}},

	// Lines: line and line-subtle are decorative dividers; control-line is
	// the boundary of an input, which must stay visible.
	{Name: "line", Light: rgb{220, 228, 240}, Dark: rgb{30, 48, 82}},
	{Name: "line-subtle", Light: rgb{236, 241, 248}, Dark: rgb{22, 38, 68}},
	{Name: "line-strong", Light: rgb{196, 208, 226}, Dark: rgb{50, 70, 108}},
	{Name: "control-line", Light: rgb{116, 129, 148}, Dark: rgb{112, 128, 150}},

	// Accent: the primary button, selected pagination, checkboxes, focus.
	{Name: "accent", Light: rgb{1, 59, 138}, Dark: rgb{1, 170, 222}},
	{Name: "accent-hover", Light: rgb{0, 44, 106}, Dark: rgb{41, 194, 240}},
	{Name: "accent-fg", Light: rgb{255, 255, 255}, Dark: rgb{11, 31, 63}},
	{Name: "accent-ui", Light: rgb{1, 59, 138}, Dark: rgb{1, 170, 222}},
	{Name: "ring", Light: rgb{1, 59, 138}, Dark: rgb{1, 170, 222}},
	{Name: "selection-bg", Light: rgb{190, 226, 244}, Dark: rgb{1, 59, 138}},
	{Name: "selection-fg", Light: rgb{11, 31, 63}, Dark: rgb{232, 246, 252}},

	// Destructive button.
	{Name: "danger-solid", Light: rgb{220, 38, 38}, Dark: rgb{220, 38, 38}, SameInBothThemes: true},
	{Name: "danger-solid-hover", Light: rgb{185, 28, 28}, Dark: rgb{185, 28, 28}, SameInBothThemes: true},
	{Name: "danger-solid-fg", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},

	// Sidebar: brand ink in light; a shade deeper than the content in dark.
	{Name: "sidebar", Light: rgb{11, 31, 63}, Dark: rgb{2, 9, 22}},
	{Name: "sidebar-fg", Light: rgb{203, 213, 225}, Dark: rgb{203, 213, 225}, SameInBothThemes: true},
	{Name: "sidebar-strong", Light: rgb{255, 255, 255}, Dark: rgb{255, 255, 255}, SameInBothThemes: true},
	{Name: "sidebar-muted", Light: rgb{148, 163, 184}, Dark: rgb{148, 163, 184}, SameInBothThemes: true},
	{Name: "sidebar-dot", Light: rgb{71, 85, 105}, Dark: rgb{71, 85, 105}, SameInBothThemes: true},
	{Name: "sidebar-accent", Light: rgb{1, 170, 222}, Dark: rgb{1, 170, 222}, SameInBothThemes: true},
	{Name: "sidebar-accent-fg", Light: rgb{11, 31, 63}, Dark: rgb{11, 31, 63}, SameInBothThemes: true},

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
