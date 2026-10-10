package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"reflect"
	"unicode/utf8"
)

// jsonWidgetTemplate is a plain multi-line text control inside an Edit
// panel, plus a Formatted panel and the tab list that switches between
// them. The tab list, the Formatted panel and the apply button start hidden
// and the shared widget script reveals them, so without JavaScript the
// field is an ordinary textarea. The leading newline after <textarea> is
// dropped by the HTML parser, so a value that itself starts with a newline
// survives the round trip.
var jsonWidgetTemplate = template.Must(template.New("jsonWidget").Parse(
	`<label for="field-{{.Name}}" class="field-label">{{.Label}}</label>
<div class="json-widget" data-tango-widget="json">
  <div class="widget-tabs" role="tablist" aria-label="{{.Label}} view" data-widget-tabs hidden>
    <button type="button" role="tab" id="field-{{.Name}}-tab-edit" class="widget-tab" aria-selected="true" aria-controls="field-{{.Name}}-panel-edit" data-tab="edit">Edit</button>
    <button type="button" role="tab" id="field-{{.Name}}-tab-view" class="widget-tab" aria-selected="false" aria-controls="field-{{.Name}}-panel-view" tabindex="-1" data-tab="view">Formatted</button>
  </div>
  <div id="field-{{.Name}}-panel-edit" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-edit" class="widget-panel">
    <textarea id="field-{{.Name}}" name="{{.Name}}" class="input json-input" rows="8" spellcheck="false" autocapitalize="off" autocorrect="off"` + ariaAttrs + `>
{{.Value}}</textarea>
  </div>
  <div id="field-{{.Name}}-panel-view" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-view" class="widget-panel" hidden>
    <pre class="json-formatted" data-json-formatted></pre>
    <button type="button" class="btn-secondary" data-json-apply hidden>Use this formatting</button>
  </div>
  <p data-json-status role="status"></p>
  <script src="{{.ScriptURL}}" defer></script>
</div>`))

type jsonWidgetData struct {
	FieldContext
	ScriptURL string
}

// jsonWidget edits a string or text field as JSON text. Parse checks syntax
// only: the submitted text is stored exactly as typed, never canonicalized,
// and an empty value is stored without being parsed.
type jsonWidget struct{}

// JSON returns tanGO's JSON Widget for a string or text field, set via
// Options.Widgets. It validates JSON syntax on save and offers a
// browser-side Formatted tab that shows the text indented; the server stays
// the authority, and the saved value is exactly the submitted text.
func JSON() Widget { return jsonWidget{} }

func (jsonWidget) Render(f FieldContext) template.HTML {
	return renderWidgetTemplate(jsonWidgetTemplate, jsonWidgetData{FieldContext: f, ScriptURL: widgetsAsset.URL})
}

// Parse stores the submitted text before checking it, so that an invalid
// save re-renders exactly what the user typed. The instance being populated
// is a fresh local value, so this changes nothing in the database until the
// whole form is valid.
func (jsonWidget) Parse(f FieldContext, form FieldValues, dest reflect.Value) error {
	if dest.Kind() != reflect.String {
		return fmt.Errorf("admin.JSON: field %q is %s, not a string", f.Name, dest.Kind())
	}
	raw := form.Get(f.Name)
	dest.SetString(raw)
	if raw == "" {
		return nil
	}
	return checkJSONSyntax(raw)
}

// checkJSONSyntax returns nil when raw is one complete JSON value, else a
// FieldError naming the first syntax problem and, when Go's decoder reports
// one, its position in characters.
func checkJSONSyntax(raw string) error {
	var doc json.RawMessage
	err := json.Unmarshal([]byte(raw), &doc)
	if err == nil {
		return nil
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		return FieldError{Message: "Invalid JSON"}
	}
	return FieldError{Message: fmt.Sprintf("Invalid JSON: %s (at character %d)", syntax.Error(), characterPosition(raw, syntax.Offset))}
}

// characterPosition converts a json.SyntaxError.Offset, which counts bytes
// read up to and including the offending byte, into the 1-based character
// position of that byte in raw.
func characterPosition(raw string, offset int64) int {
	end := int(offset) - 1
	if end < 0 {
		end = 0
	}
	if end > len(raw) {
		end = len(raw)
	}
	return utf8.RuneCountInString(raw[:end]) + 1
}
