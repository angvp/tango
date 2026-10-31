package documents

import (
	"context"
	"html/template"
	"reflect"

	"github.com/angvp/tango/admin"
)

// storedKeyWidget is the admin widget for Document.Key. The key names an
// object in storage, so the widget renders no value, keeps the stored key on
// edit whatever the form submits, and refuses create: a row without an object
// behind it would be a file that cannot be downloaded.
type storedKeyWidget struct{}

// Render shows only the field's label and a note. It never prints f.Value.
func (storedKeyWidget) Render(_ context.Context, f admin.FieldContext) template.HTML {
	return template.HTML(`<span class="field-label">` + template.HTMLEscapeString(f.Label) +
		`</span><div class="input bg-slate-50 text-slate-500">Stored with the file; not editable here.</div>`)
}

// Parse keeps the key the row already has. f.Value is that stored key on
// edit and empty on create, where the upload form must be used instead.
func (storedKeyWidget) Parse(_ context.Context, f admin.FieldContext, _ admin.FieldValues, dest reflect.Value) error {
	if f.Value == "" {
		return admin.FieldError{Message: "files are added with the upload form, not here"}
	}
	dest.SetString(f.Value)
	return nil
}
