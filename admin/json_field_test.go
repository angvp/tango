package admin_test

import (
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
)

// adminJSONDocument has a JSON field (tango:"json") next to a plain title,
// so a test can show the default widget applies only to the JSON field.
type adminJSONDocument struct {
	ID    int64 `tango:"pk"`
	Title string
	Doc   string `tango:"json"`
}

const jsonDocumentBasePath = "/admin/admin_json_document/"

// jsonDocumentMarker is a distinctive fragment of an invalid document. A
// refused save must show it only inside the textarea, never in a message.
const jsonDocumentMarker = `{"secret-marker":`

func buildJSONDocumentAdmin(t *testing.T, options admin.Options, adminOpts ...admin.Option) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminJSONDocument{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminJSONDocument{}, options); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store, adminOpts...)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler, sqlDB
}

func jsonDocumentCount(t *testing.T, sqlDB *sql.DB) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_json_document`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestJSONFieldDefaultsToTheJSONWidgetWithoutConfiguration(t *testing.T) {
	handler, _ := buildJSONDocumentAdmin(t, admin.Options{})
	body := doRequest(t, handler, http.MethodGet, jsonDocumentBasePath+"new/", nil).Body.String()

	for _, want := range []string{
		`<div class="json-widget" data-tango-widget="json">`,
		`<button type="button" role="tab" id="field-Doc-tab-view" class="widget-tab" aria-selected="false" aria-controls="field-Doc-panel-view" tabindex="-1" data-tab="view">Formatted</button>`,
		`<textarea id="field-Doc" name="Doc" class="input json-input"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("JSON field lacks %q in:\n%s", want, body)
		}
	}
	if want := `<input id="field-Title" type="text" name="Title" value="" class="input">`; !strings.Contains(body, want) {
		t.Errorf("plain field changed; want %s in:\n%s", want, body)
	}
}

func TestExplicitWidgetOverrideOnAJSONFieldWins(t *testing.T) {
	handler, _ := buildJSONDocumentAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Doc": admin.Textarea(),
	}})
	body := doRequest(t, handler, http.MethodGet, jsonDocumentBasePath+"new/", nil).Body.String()

	if strings.Contains(body, `data-tango-widget="json"`) {
		t.Errorf("the default JSON widget overrode the explicit Textarea:\n%s", body)
	}
	if !strings.Contains(body, `<textarea id="field-Doc" name="Doc" class="input" rows="4"`) {
		t.Errorf("explicit Textarea not rendered for Doc:\n%s", body)
	}
}

func TestReadOnlyJSONFieldStaysAReadonlyInput(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{ReadOnly: []string{"Doc"}})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_json_document (title, doc) VALUES ($1, $2) RETURNING id`, "t", `{"a":1}`)

	body := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", jsonDocumentBasePath, id), nil).Body.String()
	if strings.Contains(body, `data-tango-widget="json"`) {
		t.Errorf("a read-only JSON field rendered the JSON widget:\n%s", body)
	}
	want := `<input id="field-Doc" type="text" value="{&#34;a&#34;:1}" readonly class="input bg-surface-sunken text-ink-faint">`
	if !strings.Contains(body, want) {
		t.Errorf("read-only JSON field lacks %s in:\n%s", want, body)
	}
}

func TestCreateRefusesInvalidJSONThroughATextareaOverride(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Doc": admin.Textarea(),
	}})

	resp := doRequest(t, handler, http.MethodPost, jsonDocumentBasePath+"new/", url.Values{"Title": {"keep title"}, "Doc": {jsonDocumentMarker}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.Code, body)
	}
	if !strings.Contains(body, `<p class="field-error" id="field-Doc-error" role="alert">Not valid JSON.</p>`) {
		t.Errorf("no field error beside Doc:\n%s", body)
	}
	if !strings.Contains(body, `value="keep title"`) {
		t.Errorf("other input was not kept:\n%s", body)
	}
	if !strings.Contains(body, template.HTMLEscapeString(jsonDocumentMarker)) {
		t.Errorf("typed text not re-rendered:\n%s", body)
	}
	if n := strings.Count(body, "secret-marker"); n != 1 {
		t.Errorf("the document appears %d times; want once, inside the textarea only:\n%s", n, body)
	}
	if got := jsonDocumentCount(t, sqlDB); got != 0 {
		t.Fatalf("a refused save wrote a row: %d rows", got)
	}
}

func TestEditRefusesInvalidJSONThroughATextareaOverrideAndKeepsTheRow(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Doc": admin.Textarea(),
	}})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_json_document (title, doc) VALUES ($1, $2) RETURNING id`, "old", `{"ok":true}`)

	resp := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", jsonDocumentBasePath, id), url.Values{"Title": {"new"}, "Doc": {jsonDocumentMarker}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.Code, body)
	}
	if !strings.Contains(body, `<p class="field-error" id="field-Doc-error" role="alert">Not valid JSON.</p>`) {
		t.Errorf("no field error beside Doc:\n%s", body)
	}
	var title, doc string
	if err := sqlDB.QueryRow(`SELECT title, doc FROM admin_json_document WHERE id = $1`, id).Scan(&title, &doc); err != nil {
		t.Fatal(err)
	}
	if title != "old" || doc != `{"ok":true}` {
		t.Fatalf("row = %q, %q; want it unchanged", title, doc)
	}
}

func TestDefaultJSONWidgetSavesTheSubmittedTextExactly(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{})
	const doc = "{ \"b\" : 2,\n  \"a\": [1, 2] }"

	resp := doRequest(t, handler, http.MethodPost, jsonDocumentBasePath+"new/", url.Values{"Title": {"t"}, "Doc": {doc}})
	if resp.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302:\n%s", resp.Code, resp.Body.String())
	}
	var stored string
	if err := sqlDB.QueryRow(`SELECT doc FROM admin_json_document`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != doc {
		t.Fatalf("stored %q, want the submitted text %q", stored, doc)
	}
}

func TestDefaultJSONWidgetRefusesInvalidJSONAsAFieldError(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{})

	resp := doRequest(t, handler, http.MethodPost, jsonDocumentBasePath+"new/", url.Values{"Title": {"t"}, "Doc": {jsonDocumentMarker}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `id="field-Doc-error"`) {
		t.Fatalf("status = %d, want 422 with a Doc field error:\n%s", resp.Code, body)
	}
	if got := jsonDocumentCount(t, sqlDB); got != 0 {
		t.Fatalf("a refused save wrote a row: %d rows", got)
	}
}

func TestChangelistOfAJSONModelIsUnchanged(t *testing.T) {
	handler, sqlDB := buildJSONDocumentAdmin(t, admin.Options{})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_json_document (title, doc) VALUES ($1, $2) RETURNING id`, "listed", `{"a":1}`)

	resp := doRequest(t, handler, http.MethodGet, jsonDocumentBasePath, nil)
	body := resp.Body.String()
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", resp.Code, body)
	}
	if strings.Contains(body, "json-widget") || strings.Contains(body, "data-json-formatted") {
		t.Errorf("the changelist rendered JSON widget markup:\n%s", body)
	}
	if want := fmt.Sprintf(`<a href="%s%d/" class="btn-ghost" aria-label="Edit 1">Edit</a>`, jsonDocumentBasePath, id); !strings.Contains(body, want) {
		t.Errorf("the row is missing from the changelist; want %s in:\n%s", want, body)
	}
}

func TestInvalidJSONMessageIsTranslatable(t *testing.T) {
	handler, _ := buildJSONDocumentAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Doc": admin.Textarea(),
	}}, admin.WithMiddleware(adminI18NMiddleware()))
	i18n.RegisterCatalog(adminI18NLocale, map[string]string{
		"admin.error.invalid_json": "No es JSON válido.",
	})
	resp := doRequest(t, handler, http.MethodPost, jsonDocumentBasePath+"new/", url.Values{"Title": {"t"}, "Doc": {jsonDocumentMarker}})
	if body := resp.Body.String(); !strings.Contains(body, `role="alert">No es JSON válido.</p>`) {
		t.Fatalf("message not translated:\n%s", body)
	}
}
