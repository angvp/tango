package admin

import (
	"context"
	"fmt"
	"html/template"
	"reflect"
	"strings"

	"github.com/angvp/tango/internal/adminregistry"
	"github.com/angvp/tango/model"
)

// FieldContext, FieldValues, Widget, and SelectOption are defined in
// internal/adminregistry (aliased here) rather than in this package,
// because Options.Widgets needs to reference Widget without admin and
// adminregistry importing each other in a cycle — the same reason Options
// itself is an alias below.
type (
	FieldContext  = adminregistry.FieldContext
	FieldValues   = adminregistry.FieldValues
	Widget        = adminregistry.Widget
	PreviewWidget = adminregistry.PreviewWidget
	SelectOption  = adminregistry.SelectOption
)

// renderWidgetTemplate executes tmpl with data and returns the result as
// template.HTML. tmpl is always one of this file's own compiled templates
// with fixed field names, so execution can only fail for a programmer
// error caught immediately by this package's own tests, not from any data
// a caller controls.
func renderWidgetTemplate(tmpl *template.Template, data any) template.HTML {
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return template.HTML(fmt.Sprintf("<!-- tango admin: widget render error: %s -->", template.HTMLEscapeString(err.Error())))
	}
	return template.HTML(buf.String())
}

// setFieldFromReflectedString parses raw into dest according to dest's own
// kind — shared by every built-in widget whose Parse is a single
// string-to-Go-value conversion (the generic input widget and the foreign
// key select widget, which is just a numeric input with a different
// Render).
func setFieldFromReflectedString(dest reflect.Value, raw string, present bool) error {
	return setFieldFromString(dest, dest.Type(), raw, present)
}

// ariaAttrs is the accessibility wiring every built-in control shares: it
// is empty for a field with no help text and no error, so such a field's
// markup is unchanged.
const ariaAttrs = `{{if .Error}} aria-invalid="true"{{end}}{{with .DescribedBy}} aria-describedby="{{.}}"{{end}}`

var inputWidgetTemplate = template.Must(template.New("inputWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
<input id="field-{{.Name}}" type="{{.InputType}}" name="{{.Name}}" value="{{.Value}}"{{if .MaxLength}} maxlength="{{.MaxLength}}"{{end}} class="input"` + ariaAttrs + `>`))

// inputWidget is the built-in generic text/number/date-time field — one
// plain <input>, its HTML type attribute driven by the Go field's kind.
type inputWidget struct {
	InputType string
}

type inputWidgetData struct {
	FieldContext
	InputType string
}

func (w inputWidget) Render(_ context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(inputWidgetTemplate, inputWidgetData{FieldContext: f, InputType: w.InputType})
}

func (inputWidget) Parse(_ context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	return setFieldFromReflectedString(dest, form.Get(f.Name), form.Has(f.Name))
}

var checkboxWidgetTemplate = template.Must(template.New("checkboxWidget").Parse(
	`<div class="checkbox-row">
  <input type="checkbox" id="field-{{.Name}}" name="{{.Name}}" {{if .Checked}}checked{{end}} class="checkbox"` + ariaAttrs + `>
  <label for="field-{{.Name}}" class="text-sm font-medium text-ink-muted">{{.Label}}</label>
  {{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
</div>`))

// checkboxWidget is the built-in bool field: a single checkbox.
type checkboxWidget struct{}

func (checkboxWidget) Render(_ context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(checkboxWidgetTemplate, f)
}

func (checkboxWidget) Parse(_ context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	dest.SetBool(form.Has(f.Name))
	return nil
}

var selectWidgetTemplate = template.Must(template.New("selectWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
<div class="inline-row flex-wrap">
  <select id="field-{{.Name}}" name="{{.Name}}" class="input min-w-[10rem] flex-1"` + ariaAttrs + `>
    {{range .SelectOptions}}<option value="{{.Value}}" {{if .Selected}}selected{{end}}>{{.Text}}</option>{{end}}
  </select>
  {{if .RelatedCreateURL}}<a href="{{.RelatedCreateURL}}" class="btn-secondary whitespace-nowrap">{{.RelatedCreateLabel}}</a>{{end}}
</div>`))

// foreignKeySelectWidget is the built-in foreign key field: a <select>
// populated from every row of the related model. Parsing is
// identical to inputWidget's — the submitted value is still just the
// related row's primary key as a string.
type foreignKeySelectWidget struct{}

func (foreignKeySelectWidget) Render(_ context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(selectWidgetTemplate, f)
}

func (foreignKeySelectWidget) Parse(_ context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	return setFieldFromReflectedString(dest, form.Get(f.Name), form.Has(f.Name))
}

// placesOwnHelp reports whether a built-in widget renders its field's help
// text itself, between the label and the control.
func placesOwnHelp(w Widget) bool {
	switch w.(type) {
	case inputWidget, checkboxWidget, foreignKeySelectWidget, textareaWidget, readOnlyWidget, jsonWidget, markdownWidget, markdownPreviewWidget:
		return true
	}
	return false
}

// defaultWidgetForField returns tanGO's built-in Widget for field, absent
// an admin.Options.Widgets override: a foreign-key select for a field with
// an fk= tag, a checkbox for bool, otherwise a generic input typed from the
// field's Go kind.
func defaultWidgetForField(field model.FieldMeta) Widget {
	if field.ForeignKey != "" {
		return foreignKeySelectWidget{}
	}
	if field.Type.Kind() == reflect.Bool {
		return checkboxWidget{}
	}
	if field.JSON {
		return jsonWidget{}
	}
	return inputWidget{InputType: inputTypeForKind(field.Type)}
}

var textareaWidgetTemplate = template.Must(template.New("textareaWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
<textarea id="field-{{.Name}}" name="{{.Name}}" class="input" rows="4"` + ariaAttrs + `>{{.Value}}</textarea>`))

// textareaWidget is a built-in multi-line text field, for a string field
// that needs more room than the default single-line input. Unlike the
// other built-ins, it's never a field's default — an app opts into it via
// Options.Widgets (see Textarea).
type textareaWidget struct{}

func (textareaWidget) Render(_ context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(textareaWidgetTemplate, f)
}

func (textareaWidget) Parse(_ context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	dest.SetString(form.Get(f.Name))
	return nil
}

// Textarea returns tanGO's built-in multi-line text Widget. Set it on a
// string field via Options.Widgets, e.g. Options{Widgets: map[string]Widget{
// "Body": admin.Textarea()}}.
func Textarea() Widget { return textareaWidget{} }

var readOnlyWidgetTemplate = template.Must(template.New("readOnlyWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
<input id="field-{{.Name}}" type="text" value="{{.Value}}" readonly class="input bg-surface-sunken text-ink-faint"` + ariaAttrs + `>`))

// readOnlyWidget renders any read-only field as a readonly text input, so
// its value can be focused, selected and copied. The input has no name, so
// the browser never submits it; regardless of what Widget it
// would otherwise use — Options.ReadOnly always wins, uniformly, rather
// than asking every Widget implementation to know how to render itself
// non-editably. It never parses: a read-only field's submitted data is
// always ignored (see admin.Options.ReadOnly).
type readOnlyWidget struct{}

func (readOnlyWidget) Render(_ context.Context, f FieldContext) template.HTML {
	return renderWidgetTemplate(readOnlyWidgetTemplate, f)
}

func (readOnlyWidget) Parse(context.Context, FieldContext, FieldValues, reflect.Value) error {
	return nil
}
