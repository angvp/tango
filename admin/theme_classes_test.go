package admin

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// rawPaletteClass matches a Tailwind utility that names a raw palette colour
// (slate-500, indigo-600, white) instead of a semantic token from theme.go.
// Side modifiers (border-b-white) and opacity suffixes (bg-slate-950/50) are
// part of the match.
// The class must end at a non-name character, so neutral-bg (a token that
// shares a word with Tailwind's neutral palette) is not a raw class.
var rawPaletteClass = regexp.MustCompile(`\b(?:bg|text|border|ring|ring-offset|placeholder|from|to|divide|fill|stroke)(?:-[trblxy])?-(?:slate|gray|zinc|neutral|stone|indigo|red|emerald|green|amber|yellow|sky|blue|rose|violet|purple|teal|cyan|white|black)(?:-\d{2,3})?(?:/\d+)?(?:[^\w-]|$)`)

// themeOwnedFiles are the admin sources whose templates and CSS this slice
// migrated to semantic tokens. render.go, widget.go, fields.go, markdown.go,
// dashboard_*.go and login.go belong to later slices and are not listed.
var themeOwnedFiles = []string{
	"layout.go",
	"index.go",
	"list.go",
	"edit.go",
	"create.go",
	"delete.go",
	"display.go",
}

func TestRawPaletteClassPatternCatchesRawUtilities(t *testing.T) {
	tests := []struct {
		class string
		want  bool
	}{
		{"text-slate-500", true},
		{"hover:bg-indigo-500", true},
		{"group-hover:bg-slate-400", true},
		{"bg-slate-950/50", true},
		{"border-b-white", true},
		{"ring-offset-white", true},
		{"placeholder:text-slate-400", true},
		{"bg-white/85", true},
		{"text-white", true},
		{"bg-neutral-bg", false},
		{"text-neutral-fg", false},
		{"bg-surface", false},
		{"text-ink-muted", false},
		{"bg-sidebar/50", false},
		{"border-control-line", false},
		{"text-accent-fg", false},
		{"ring-ring", false},
	}
	for _, tt := range tests {
		if got := rawPaletteClass.MatchString(tt.class); got != tt.want {
			t.Errorf("rawPaletteClass.MatchString(%q) = %v, want %v", tt.class, got, tt.want)
		}
	}
}

func TestOwnedAdminTemplatesUseNoRawPaletteClasses(t *testing.T) {
	for _, name := range themeOwnedFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if m := rawPaletteClass.FindString(line); m != "" {
				t.Errorf("%s:%d uses raw palette class %q; use a semantic token from theme.go", name, i+1, m)
			}
		}
	}
}
