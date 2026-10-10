package admin

import (
	"context"
	"fmt"
	"html/template"
	"reflect"
	"unicode/utf8"
)

// MarkdownPreview renders a Markdown source to HTML for the Markdown widget's
// preview. The host owns rendering and sanitization policy: tanGO ships no
// Markdown parser and does not sanitize what the callback returns.
type MarkdownPreview func(context.Context, string) (template.HTML, error)

type markdownConfig struct {
	preview MarkdownPreview
}

// MarkdownOption configures the Markdown widget built by Markdown.
type MarkdownOption func(*markdownConfig)

// WithMarkdownPreview sets the host's renderer for the Markdown widget's
// preview. A widget without one is a plain Markdown editor.
func WithMarkdownPreview(preview MarkdownPreview) MarkdownOption {
	return func(cfg *markdownConfig) { cfg.preview = preview }
}

// Markdown returns tanGO's Markdown Widget: a monospace, resizable textarea
// with a character count. Set it on a string field via Options.Widgets, e.g.
// Options{Widgets: map[string]Widget{"Bio": admin.Markdown()}}. It stores the
// submitted text exactly as typed.
func Markdown(opts ...MarkdownOption) Widget {
	var cfg markdownConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return markdownWidget{preview: cfg.preview}
}

// markdownWidget is the widget Markdown returns. preview is stored for the
// preview endpoint and is not used by Render or Parse.
type markdownWidget struct {
	preview MarkdownPreview
}

var markdownWidgetTemplate = template.Must(template.New("markdownWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
<textarea id="field-{{.Name}}" name="{{.Name}}" class="input font-mono resize-y" rows="12" data-tango-widget="markdown"{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}` + ariaAttrs + `>
{{.Value}}</textarea>
<p id="field-{{.Name}}-count" class="text-xs text-slate-500 mt-1" aria-live="polite">{{.Count}}</p>`))

// markdownWidgetData is what markdownWidgetTemplate renders: the field plus
// its character count, computed on the server.
type markdownWidgetData struct {
	FieldContext
	Count string
}

func (markdownWidget) Render(f FieldContext) template.HTML {
	return renderWidgetTemplate(markdownWidgetTemplate, markdownWidgetData{
		FieldContext: f,
		Count:        markdownCountText(f.Value, f.MaxLength),
	})
}

func (markdownWidget) Parse(f FieldContext, form FieldValues, dest reflect.Value) error {
	dest.SetString(form.Get(f.Name))
	return nil
}

// markdownCountText describes value's length in characters (runes, not
// bytes), with the declared maximum when the field has one.
func markdownCountText(value string, maxLength int) string {
	n := utf8.RuneCountInString(value)
	if maxLength > 0 {
		return fmt.Sprintf("%d / %d characters", n, maxLength)
	}
	return fmt.Sprintf("%d characters", n)
}
