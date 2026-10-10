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
	if cfg.preview == nil {
		return markdownWidget{}
	}
	return markdownPreviewWidget{preview: cfg.preview}
}

// markdownWidget is the editor without a preview callback. Its markup is
// the plain textarea and count, and it is not a PreviewWidget.
type markdownWidget struct{}

// markdownPreviewWidget is the editor with a host preview callback: the same
// markup plus a Preview control, and the PreviewWidget that admin's preview
// route calls.
type markdownPreviewWidget struct {
	preview MarkdownPreview
}

// Preview renders source with the host's callback. Admin applies the
// deadline and the output limit; the callback owns rendering and
// sanitization.
func (w markdownPreviewWidget) Preview(ctx context.Context, source string) (template.HTML, error) {
	return w.preview(ctx, source)
}

// markdownWidgetTemplate is the editor. The Preview controls are appended
// only when Preview is set, so the markup of a widget without a callback is
// exactly the plain textarea and count. The controls start hidden and the
// shared widget script reveals them, so without JavaScript the field is an
// ordinary textarea.
var markdownWidgetTemplate = template.Must(template.New("markdownWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
<textarea id="field-{{.Name}}" name="{{.Name}}" class="input font-mono resize-y" rows="12" data-tango-widget="markdown"{{if .Preview}} data-preview-url="../_preview/{{.Name}}/"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}` + ariaAttrs + `>
{{.Value}}</textarea>
<p id="field-{{.Name}}-count" class="text-xs text-slate-500 mt-1" aria-live="polite">{{.Count}}</p>{{if .Preview}}
<div class="flex items-center gap-2 mt-2">
  <button type="button" id="field-{{.Name}}-preview-button" class="btn-secondary" data-markdown-preview aria-controls="field-{{.Name}}-preview" hidden>Preview</button>
  <p id="field-{{.Name}}-preview-status" role="status" class="text-xs text-slate-500" data-markdown-status></p>
</div>
<iframe id="field-{{.Name}}-preview" title="Preview of {{.Label}}" sandbox="" referrerpolicy="no-referrer" class="w-full min-h-40 mt-2 rounded border border-slate-300 bg-white" hidden></iframe>{{end}}`))

// markdownWidgetData is what markdownWidgetTemplate renders: the field plus
// its character count, computed on the server, and whether the preview
// controls are present.
type markdownWidgetData struct {
	FieldContext
	Count   string
	Preview bool
}

func (w markdownWidget) Render(f FieldContext) template.HTML {
	return renderMarkdown(f, false)
}

func (w markdownWidget) Parse(f FieldContext, form FieldValues, dest reflect.Value) error {
	return parseMarkdown(f, form, dest)
}

func renderMarkdown(f FieldContext, preview bool) template.HTML {
	return renderWidgetTemplate(markdownWidgetTemplate, markdownWidgetData{
		FieldContext: f,
		Count:        markdownCountText(f.Value, f.MaxLength),
		Preview:      preview,
	})
}

func parseMarkdown(f FieldContext, form FieldValues, dest reflect.Value) error {
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

func (w markdownPreviewWidget) Render(f FieldContext) template.HTML {
	return renderMarkdown(f, true)
}

func (w markdownPreviewWidget) Parse(f FieldContext, form FieldValues, dest reflect.Value) error {
	return parseMarkdown(f, form, dest)
}
