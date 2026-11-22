package admin

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"reflect"

	"github.com/angvp/tango/storage"
)

// removeSuffix is appended to a file field's name to name its Remove checkbox.
const removeSuffix = "__remove"

// ErrInvalidFile is returned when a file field is set up wrongly: on a field
// that is not a string, or with no positive size limit.
var ErrInvalidFile = errors.New("admin: invalid file field")

// FileOptions configure the file widget of one string field.
type FileOptions struct {
	// Put is how an upload is checked and stored. MaxSize is required and
	// must be positive; AllowedTypes is optional.
	Put storage.PutOptions
	// Store receives this field's files. Nil means the registry's store
	// (Registry.SetStorage), else a local directory.
	Store storage.Store
	// URL returns where a stored file is served, for the link the form shows
	// beside the field. Nil means the store's own public URL, when it has one
	// (storage.PublicURLer); with neither, the key is shown without a link.
	URL func(key string) string
}

// FileRules is what the admin needs from a widget that takes a file.
type FileRules struct {
	Put   storage.PutOptions
	Store storage.Store
	URL   func(key string) string
}

// FileWidget is the optional extension a Widget implements when its field
// takes an uploaded file. A model form that has one is sent as
// multipart/form-data and read with storage.ReadForm; every other widget
// still gets its values through FieldValues. The field holds the object's
// generated key.
type FileWidget interface {
	FileRules() FileRules
}

type fileWidget struct{ options FileOptions }

// File returns the file Widget for a string field, set via Options.Widgets.
// The staff user picks a file in the form; it goes to the store and the field
// holds its generated key. No new file keeps the current one, a new file
// replaces it, and the Remove checkbox clears it. An old object is deleted
// only after the row is saved.
func File(options FileOptions) Widget { return fileWidget{options: options} }

func (w fileWidget) FileRules() FileRules {
	return FileRules{Put: w.options.Put, Store: w.options.Store, URL: w.options.URL}
}

var fileWidgetTemplate = template.Must(template.New("fileWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
{{if .HelpText}}<p class="field-help" id="field-{{.Name}}-help">{{.HelpText}}</p>{{end}}
{{if .Value}}<p class="hint-small" id="field-{{.Name}}-current">{{.CurrentLabel}} {{if .FileURL}}<a href="{{.FileURL}}">{{.Value}}</a>{{else}}{{.Value}}{{end}}</p>{{end}}
<input type="file" id="field-{{.Name}}" name="{{.Name}}" class="input"` + ariaAttrs + `>
{{if .Value}}<div class="checkbox-row">
  <input type="checkbox" id="field-{{.Name}}` + removeSuffix + `" name="{{.Name}}` + removeSuffix + `" class="checkbox">
  <label for="field-{{.Name}}` + removeSuffix + `" class="text-sm font-medium text-ink-muted">{{.RemoveLabel}}</label>
</div>{{end}}`))

type fileWidgetData struct {
	FieldContext
	CurrentLabel string
	RemoveLabel  string
}

func (fileWidget) Render(ctx context.Context, f FieldContext) template.HTML {
	text := newWidgetText(ctx)
	return renderWidgetTemplate(fileWidgetTemplate, fileWidgetData{
		FieldContext: f,
		CurrentLabel: text.T("admin.file.current", "Current file:"),
		RemoveLabel:  text.T("admin.file.remove", "Remove the current file"),
	})
}

// uploadedKeys is implemented by the form the admin hands a file widget after
// it stored the request's files. The keys never travel as form values, so a
// request cannot name a key of its own.
type uploadedKeys interface {
	UploadedKey(field string) (string, bool)
}

// Parse chooses the field's key: a newly stored file wins, then the Remove
// checkbox clears it, else the current key stays. The object itself was
// already stored when the form was read.
func (fileWidget) Parse(_ context.Context, f FieldContext, form FieldValues, dest reflect.Value) error {
	if dest.Kind() != reflect.String {
		return fmt.Errorf("admin.File: field %q is %s, not a string", f.Name, dest.Kind())
	}
	if uploads, ok := form.(uploadedKeys); ok {
		if key, ok := uploads.UploadedKey(f.Name); ok {
			dest.SetString(key)
			return nil
		}
	}
	if form.Has(f.Name + removeSuffix) {
		dest.SetString("")
		return nil
	}
	dest.SetString(f.Value)
	return nil
}
