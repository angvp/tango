package admin

import (
	"context"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"
)

// rawColourClass matches a Tailwind utility that names a palette colour
// directly (bg-slate-50, text-red-600, hover:bg-red-50, border-white). Such a
// class ignores the theme, so widgets use the semantic tokens from
// admin/tailwind/admin.css instead.
var rawColourClass = regexp.MustCompile(`(?:^|[\s:"])(?:bg|text|border|ring|divide|outline|placeholder|from|to|via|fill|stroke|accent|caret|shadow)-(?:white|black|slate|gray|zinc|neutral|stone|red|green|emerald|amber|yellow|blue|indigo|sky|rose|orange|violet|purple|teal|cyan|lime|pink|fuchsia)(?:-\d+)?(?:[\s"/]|$)`)

// markdownPreviewCanvasClass is the class list of the Markdown preview iframe.
// It is the one raw colour a widget keeps: bg-white is a fixed light canvas in
// both themes (D8), documented in markdown.go.
const markdownPreviewCanvasClass = `class="markdown-preview-frame w-full min-h-40 mt-2 rounded border border-line-strong bg-white"`

// widgetSourceFiles are the files this ticket owns. Dashboard, layout, login
// and the render helpers belong to other tickets.
var widgetSourceFiles = []string{
	"widget.go",
	"fields.go",
	"markdown.go",
	"href.go",
	"json_widget.go",
	"markdown_preview.go",
}

// renderedWidgets renders each built-in widget template the admin emits,
// including the Markdown editor with its preview panel.
func renderedWidgets() map[string]template.HTML {
	field := FieldContext{Name: "Body", Label: "Body", Value: "text", Checked: true}
	return map[string]template.HTML{
		"input":           inputWidget{InputType: "text"}.Render(context.Background(), field),
		"checkbox":        checkboxWidget{}.Render(context.Background(), field),
		"textarea":        textareaWidget{}.Render(context.Background(), field),
		"select":          foreignKeySelectWidget{}.Render(context.Background(), field),
		"readonly":        readOnlyWidget{}.Render(context.Background(), field),
		"json":            jsonWidget{}.Render(context.Background(), field),
		"markdown":        markdownWidget{}.Render(context.Background(), field),
		"markdownPreview": renderMarkdown(context.Background(), field, true),
	}
}

func TestWidgetsRenderNoRawColourClasses(t *testing.T) {
	for name, html := range renderedWidgets() {
		markup := string(html)
		if name == "markdownPreview" {
			markup = strings.Replace(markup, markdownPreviewCanvasClass, "", 1)
		}
		if m := rawColourClass.FindString(markup); m != "" {
			t.Errorf("%s widget renders raw colour class %q:\n%s", name, strings.TrimSpace(m), html)
		}
	}
}

func TestWidgetSourcesUseNoRawColourClasses(t *testing.T) {
	for _, file := range widgetSourceFiles {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !rawColourClass.MatchString(line) {
				continue
			}
			if file == "markdown.go" && strings.Contains(line, markdownPreviewCanvasClass) {
				continue
			}
			t.Errorf("%s:%d uses a raw colour class; use a token from admin/tailwind/admin.css: %s", file, i+1, strings.TrimSpace(line))
		}
	}
}

func TestMarkdownPreviewCanvasIsDeliberatelyLight(t *testing.T) {
	markup := string(renderMarkdown(t.Context(), FieldContext{Name: "Body", Label: "Body"}, true))
	if !strings.Contains(markup, markdownPreviewCanvasClass) {
		t.Fatalf("preview iframe lost its fixed white canvas or its themed frame:\n%s", markup)
	}
	if !strings.Contains(markup, `sandbox=""`) {
		t.Fatalf("preview iframe must stay sandboxed:\n%s", markup)
	}
}

func TestSecondaryTextPassesContrastOnSunkenSurfaceInBothThemes(t *testing.T) {
	// ink-faint is the read-only value and the field counters; theme_test.go
	// declares its pairs on surface only, so the sunken surface is checked here.
	for _, dark := range []bool{false, true} {
		fg := tokenValue(t, "ink-faint", dark)
		bg := tokenValue(t, "surface-sunken", dark)
		if got := contrast(fg, bg); got < 4.5 {
			t.Errorf("dark=%v: ink-faint on surface-sunken = %.2f:1, want at least 4.5:1", dark, got)
		}
	}
}
