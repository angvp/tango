package adminregistry

import (
	"html/template"
	"reflect"
)

// SelectOption is one <option> in a foreign key field's <select>.
type SelectOption struct {
	Value    string
	Text     string
	Selected bool
}

// FieldContext carries what a Widget needs to render and parse one model
// field's create/edit form control. The same FieldContext is passed to both
// Render and Parse, so a widget can derive its own submitted form key(s)
// from Name identically on both sides — e.g. a multi-input widget might use
// Name+"_date" and Name+"_time" — rather than a render-time and parse-time
// convention drifting apart.
type FieldContext struct {
	// Name is the model field's Go name — also the base name a simple
	// widget reads directly from the submitted form, and the stable prefix
	// a multi-input widget derives its own input names from.
	Name string
	// Label is the field's display label (from Options.Labels, or a
	// humanized default).
	Label string
	// HelpText is optional descriptive text for the field (from
	// Options.HelpText); empty if unset.
	HelpText string
	// Value is the field's current value, formatted for display. Empty for
	// a new (zero) instance.
	Value string
	// Checked is the field's current boolean value, for checkbox-shaped
	// widgets.
	Checked bool
	// ReadOnly reports whether the field should render non-editably and
	// ignore submitted form data entirely (Options.ReadOnly). A Widget set
	// via Options.Widgets is never consulted for a ReadOnly field — the
	// admin package renders a single generic read-only presentation
	// instead, uniformly, regardless of which Widget the field would
	// otherwise use.
	ReadOnly bool
	// SelectOptions lists every row of the related model as a value/label
	// pair, populated only for a foreign key field whose related model is
	// registered — nil otherwise.
	SelectOptions []SelectOption
	// RelatedCreateURL links to the related model's admin create page for
	// foreign-key fields whose related model is also admin-registered.
	// Empty means no quick-create affordance should render.
	RelatedCreateURL string
	// RelatedCreateLabel is the accessible label for RelatedCreateURL.
	// Empty means widgets may fall back to their own default.
	RelatedCreateLabel string
}

// FieldValues is the whole submitted form, so a Widget's Parse can read
// more than one input for a single Go field (see FieldContext.Name).
type FieldValues interface {
	Get(key string) string
	Has(key string) bool
}

// Widget renders one model field's create/edit form control and parses its
// submitted value back into the Go field, replacing tanGO's default
// behavior for that field alone — nothing else about the form changes.
// tanGO's built-in field behaviors (a generic text/number/date-time input,
// a checkbox, a textarea, and the foreign-key select) are themselves
// Widgets, set via Options.Widgets like any custom one.
//
// This interface, tanGO's built-in widgets, and Options' Widgets/Labels/
// HelpText/ReadOnly/FieldOrder fields are best-effort, not a stable v0.0.1
// contract — see docs/limitations.md. A widget's
// Render output may include its own <link>/<script> tags for CSS/JS
// (served via its owning app's own embed.FS route, per the convention
// reusable apps already use for static assets); since there is no shared
// deduplication, that output must be idempotent — safe to emit once per
// rendered instance — because the same widget on multiple fields will
// render its tags more than once.
type Widget interface {
	Render(f FieldContext) template.HTML
	Parse(f FieldContext, form FieldValues, dest reflect.Value) error
}
