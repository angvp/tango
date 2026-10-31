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
<div class="json-widget" data-tango-widget="json"{{if .Text.Active}} data-msg-nothing="{{.Text.T "admin.json.status_nothing" "Nothing to format."}}" data-msg-invalid="{{.Text.T "admin.json.status_invalid" "Not valid JSON."}}" data-msg-invalid-detail="{{.Text.T "admin.json.view_invalid" "Not valid JSON: {error}"}}" data-msg-applied="{{.Text.T "admin.json.status_applied" "Formatting applied."}}"{{end}}>
  <div class="widget-tabs" role="tablist" aria-label="{{.Text.T "admin.widget.view_label" "%s view" .Label}}" data-widget-tabs hidden>
    <button type="button" role="tab" id="field-{{.Name}}-tab-edit" class="widget-tab" aria-selected="true" aria-controls="field-{{.Name}}-panel-edit" data-tab="edit">{{.Text.T "admin.json.tab_edit" "Edit"}}</button>
    <button type="button" role="tab" id="field-{{.Name}}-tab-view" class="widget-tab" aria-selected="false" aria-controls="field-{{.Name}}-panel-view" tabindex="-1" data-tab="view">{{.Text.T "admin.json.tab_formatted" "Formatted"}}</button>
  </div>
  <div id="field-{{.Name}}-panel-edit" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-edit" class="widget-panel">
    <textarea id="field-{{.Name}}" name="{{.Name}}" class="input json-input" rows="8" spellcheck="false" autocapitalize="off" autocorrect="off"` + ariaAttrs + `>
{{.Value}}</textarea>
  </div>
  <div id="field-{{.Name}}-panel-view" role="tabpanel" aria-labelledby="field-{{.Name}}-tab-view" class="widget-panel" hidden>
    <pre class="json-formatted" data-json-formatted></pre>
    <button type="button" class="btn-secondary" data-json-apply hidden>{{.Text.T "admin.json.apply" "Use this formatting"}}</button>
  </div>
  <p data-json-status role="status"></p>
  <script src="{{.ScriptURL}}" defer></script>
</div>`))

type jsonWidgetData struct {
	FieldContext
	Text      widgetText
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
	return renderWidgetTemplate(jsonWidgetTemplate, jsonWidgetData{FieldContext: f, Text: newWidgetText(f), ScriptURL: widgetsAsset.URL})
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

// notValidJSON is the message for JSON text that fails to parse. The decoder's
// own text is never shown: it quotes the document and reads as Go.
const notValidJSON = "This is not valid JSON."

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
		return FieldError{Message: notValidJSON, text: &localizedText{key: "admin.error.json_invalid", fallback: notValidJSON}}
	}
	const fallback = "This is not valid JSON (problem at character %d)."
	args := []any{characterPosition(raw, syntax.Offset)}
	return FieldError{
		Message: fmt.Sprintf(fallback, args...),
		text:    &localizedText{key: "admin.error.json_position", fallback: fallback, args: args},
	}
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
