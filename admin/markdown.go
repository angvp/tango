package admin

import (
	"context"
	"html/template"
	"reflect"
	"strconv"
	"strings"
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

// markdownWidgetTemplate is the editor. A widget with a preview callback
// puts the textarea and count in a Write panel and adds a Preview tab and
// panel; a widget without one is exactly the plain textarea and count. The
// tab list and the Preview panel start hidden and the shared widget script
// reveals them, so without JavaScript the field is an ordinary textarea.
//
// The preview iframe is the one deliberate exception to the token rule. Its
// canvas is a fixed white (bg-white) in both themes, inside a themed frame
// (border-line-strong). The preview is an isolated, sandboxed, light surface:
// the host's rendered HTML is not styled by the admin, so it is not promised
// to match any public site, and the host owns its own dark-mode behaviour.
var markdownWidgetTemplate = template.Must(template.New("markdownWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>{{if .Preview}}
<div class="widget-tabs" role="tablist" aria-label="{{.Text.T "admin.widget.view_label" "%s view" .Label}}" data-widget-tabs hidden>
  <button type="button" role="tab" id="field-{{.Name}}-tab-write" class="widget-tab" aria-selected="true" aria-controls="field-{{.Name}}-panel-write" data-tab="write">{{.Text.T "admin.markdown.tab_write" "Write"}}</button>
  <button type="button" role="tab" id="field-{{.Name}}-tab-preview" class="widget-tab" aria-selected="false" aria-controls="field-{{.Name}}-panel-preview" tabindex="-1" data-tab="preview">{{.Text.T "admin.markdown.tab_preview" "Preview"}}</button>
</div>
<div id="field-{{.Name}}-panel-write" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-write" class="widget-panel">{{end}}
<textarea id="field-{{.Name}}" name="{{.Name}}" class="input font-mono resize-y" rows="12" data-tango-widget="markdown"{{if .Preview}} data-preview-url="{{.PreviewURL}}"{{end}}{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}}{{if .Text.Active}} data-msg-count="{{.Text.T "admin.markdown.count" "{count} characters"}}" data-msg-count-max="{{.Text.T "admin.markdown.count_max" "{count} / {max} characters"}}"{{if .Preview}} data-msg-preview-failed="{{.Text.T "admin.markdown.preview_failed" "Preview failed."}}" data-msg-preview-unavailable="{{.Text.T "admin.markdown.preview_unavailable" "Preview unavailable. Reload the page and try again."}}" data-msg-preview-rendering="{{.Text.T "admin.markdown.preview_rendering" "Rendering preview..."}}"{{end}}{{end}}` + ariaAttrs + `>
{{.Value}}</textarea>
<p id="field-{{.Name}}-count" class="hint-small mt-1" aria-live="polite">{{.Count}}</p>{{if .Preview}}
</div>
<div id="field-{{.Name}}-panel-preview" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-preview" class="widget-panel" hidden>
  <p id="field-{{.Name}}-preview-status" aria-live="polite" class="hint-small" data-markdown-status></p>
  <iframe id="field-{{.Name}}-preview" title="{{.Text.T "admin.markdown.preview_title" "Preview of %s" .Label}}" sandbox="" referrerpolicy="no-referrer" class="w-full min-h-40 mt-2 rounded border border-line-strong bg-white"></iframe>
</div>{{end}}`))

// markdownWidgetData is what markdownWidgetTemplate renders: the field plus
// its character count, computed on the server, and whether the preview
// controls are present.
type markdownWidgetData struct {
	FieldContext
	Text    widgetText
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
	text := newWidgetText(f)
	return renderWidgetTemplate(markdownWidgetTemplate, markdownWidgetData{
		FieldContext: f,
		Text:         text,
		Count:        markdownCountText(text, f.Value, f.MaxLength),
		Preview:      preview,
	})
}

func parseMarkdown(f FieldContext, form FieldValues, dest reflect.Value) error {
	dest.SetString(form.Get(f.Name))
	return nil
}

// markdownCountText describes value's length in characters (runes, not
// bytes), with the declared maximum when the field has one. The text comes
// from the same {count} and {max} patterns the widget script reads, so the
// server and the browser word it alike.
func markdownCountText(text widgetText, value string, maxLength int) string {
	n := strconv.Itoa(utf8.RuneCountInString(value))
	if maxLength > 0 {
		pattern := text.T("admin.markdown.count_max", "{count} / {max} characters")
		return strings.NewReplacer("{count}", n, "{max}", strconv.Itoa(maxLength)).Replace(pattern)
	}
	return strings.NewReplacer("{count}", n).Replace(text.T("admin.markdown.count", "{count} characters"))
}

func (w markdownPreviewWidget) Render(f FieldContext) template.HTML {
	return renderMarkdown(f, true)
}

func (w markdownPreviewWidget) Parse(f FieldContext, form FieldValues, dest reflect.Value) error {
	return parseMarkdown(f, form, dest)
}
