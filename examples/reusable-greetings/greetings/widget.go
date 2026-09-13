package greetings

import (
	"html/template"
	"reflect"
	"strings"

	"github.com/angvp/tango/admin"
)

// trimmedNameWidgetTemplate renders a plain text input for Name, plus this
// app's own JS asset — served via its own embed.FS route (see serveStatic),
// the same convention Milestone 13 established for app-owned static
// assets. This demonstrates that a reusable app needs no new framework API
// to contribute an admin.Widget: it's a plain Go type implementing
// admin.Widget, set via admin.Options.Widgets in the app's own Register.
var trimmedNameWidgetTemplate = template.Must(template.New("trimmedName").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
<input id="field-{{.Name}}" type="text" name="{{.Name}}" value="{{.Value}}" class="input">
<script src="/greetings/static/widget.js" defer></script>`))

// trimmedNameWidget is greetings' own admin.Widget for its Name field.
// Parsing trims leading/trailing whitespace — a real, if modest, behavior
// that proves Parse actually runs, distinct from the built-in generic
// input's plain pass-through.
type trimmedNameWidget struct{}

func (trimmedNameWidget) Render(f admin.FieldContext) template.HTML {
	var buf strings.Builder
	// trimmedNameWidgetTemplate is a fixed, compile-time-checked template
	// executed with admin.FieldContext's own fields — Execute cannot fail
	// here for any value this widget is ever called with.
	_ = trimmedNameWidgetTemplate.Execute(&buf, f)
	return template.HTML(buf.String())
}

func (trimmedNameWidget) Parse(f admin.FieldContext, form admin.FieldValues, dest reflect.Value) error {
	dest.SetString(strings.TrimSpace(form.Get(f.Name)))
	return nil
}

// NameWidget is greetings' contributed admin.Widget for its Name field —
// exported so a host project could reuse it on a field of its own, though
// Register below is what actually wires it into this app's own admin
// registration.
func NameWidget() admin.Widget { return trimmedNameWidget{} }
