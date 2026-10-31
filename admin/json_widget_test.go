package admin_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// buildJSONNoteAdmin registers adminNote with its Body edited by admin.JSON
// and returns the handler plus the database, so a test can read rows back.
func buildJSONNoteAdmin(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminNote{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminNote{}, admin.Options{Widgets: map[string]admin.Widget{
		"Body": admin.JSON(),
	}}); err != nil {
		t.Fatal(err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler, sqlDB
}

// jsonTextareaPattern matches the Body textarea and captures its content.
var jsonTextareaPattern = regexp.MustCompile(`(?s)<textarea id="field-Body" name="Body" class="input json-input"[^>]*>\n(.*?)</textarea>`)

// submittedJSONBody returns the text inside the Body textarea, unescaped.
func submittedJSONBody(t *testing.T, body string) string {
	t.Helper()
	m := jsonTextareaPattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no JSON textarea in:\n%s", body)
	}
	return htmlUnescape(m[1])
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">", "&amp;", "&").Replace(s)
}

func TestJSONWidgetRendersAPlainTextareaWithHooksAndNoInlineScript(t *testing.T) {
	handler, _ := buildJSONNoteAdmin(t)
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	for _, want := range []string{
		`<div class="json-widget" data-tango-widget="json" data-msg-invalid-detail="This is not valid JSON. Check that commas, quotes and brackets are in place.">`,
		`<div class="widget-tabs" role="tablist" aria-label="Body view" data-widget-tabs hidden>`,
		`<button type="button" role="tab" id="field-Body-tab-edit" class="widget-tab" aria-selected="true" aria-controls="field-Body-panel-edit" data-tab="edit">Edit</button>`,
		`<button type="button" role="tab" id="field-Body-tab-view" class="widget-tab" aria-selected="false" aria-controls="field-Body-panel-view" tabindex="-1" data-tab="view">Formatted</button>`,
		`<div id="field-Body-panel-edit" role="tabpanel" aria-labelledby="field-Body-tab-edit" class="widget-panel">`,
		`<div id="field-Body-panel-view" role="tabpanel" aria-labelledby="field-Body-tab-view" class="widget-panel">`,
		`<pre class="json-formatted" data-json-formatted></pre>`,
		`<button type="button" class="btn-secondary" data-json-apply hidden>Use this formatting</button>`,
		`<p class="hint-small" data-json-status aria-live="polite"></p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "data-json-format hidden") {
		t.Errorf("the Format button is replaced by the Formatted tab:\n%s", body)
	}
	src := regexp.MustCompile(`<script src="(/admin/_assets/widgets\.[0-9a-f]{16}\.js)" defer></script>`).FindStringSubmatch(body)
	if src == nil {
		t.Fatalf("no hashed widget script tag in:\n%s", body)
	}
	if got := submittedJSONBody(t, body); got != "" {
		t.Errorf("new textarea content = %q, want empty", got)
	}
}

func TestJSONWidgetSavesTheExactSubmittedText(t *testing.T) {
	handler, sqlDB := buildJSONNoteAdmin(t)
	// Deliberately not canonical: spacing, key order and a trailing newline survive.
	submitted := "{\n  \"theme\" :\"dark\",  \"tags\": [1,2]\n}\n"
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {submitted}})
	if resp.Code != http.StatusFound {
		t.Fatalf("status %d, want 302:\n%s", resp.Code, resp.Body.String())
	}
	var stored string
	if err := sqlDB.QueryRow(`SELECT body FROM admin_note`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != submitted {
		t.Errorf("stored %q, want exactly %q", stored, submitted)
	}
}

func TestJSONWidgetEmptyValueIsSavedWithoutParsing(t *testing.T) {
	handler, sqlDB := buildJSONNoteAdmin(t)
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {""}})
	if resp.Code != http.StatusFound {
		t.Fatalf("status %d, want 302:\n%s", resp.Code, resp.Body.String())
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_note WHERE body = ''`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("empty body rows = %d, %v; want 1", n, err)
	}
}

// fieldErrorParagraph returns the text of the error paragraph for field, so an
// assertion can read what the message says without matching the textarea,
// which re-renders the submitted document on purpose.
func fieldErrorParagraph(t *testing.T, body, field string) string {
	t.Helper()
	open := `<p class="field-error" id="field-` + field + `-error"`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("no error paragraph for %s:\n%s", field, body)
	}
	rest := body[start:]
	end := strings.Index(rest, "</p>")
	if end < 0 {
		t.Fatalf("unterminated error paragraph for %s:\n%s", field, body)
	}
	text := rest[:end]
	return text[strings.Index(text, ">")+1:]
}

func TestJSONWidgetServerErrorNeverQuotesTheDocumentGuard(t *testing.T) {
	handler, _ := buildJSONNoteAdmin(t)
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {`{"secret": oops}`}})
	message := fieldErrorParagraph(t, resp.Body.String(), "Body")
	for _, leaked := range []string{"secret", "oops", "{"} {
		if strings.Contains(message, leaked) {
			t.Errorf("error paragraph %q quotes the document (%q)", message, leaked)
		}
	}
}

func TestJSONWidgetRefusesInvalidJSONAndKeepsTheSubmittedText(t *testing.T) {
	handler, sqlDB := buildJSONNoteAdmin(t)
	submitted := `{"a": 1,}`
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {submitted}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422:\n%s", resp.Code, body)
	}
	if got := submittedJSONBody(t, body); got != submitted {
		t.Errorf("re-rendered text = %q, want the exact submitted %q", got, submitted)
	}
	if !strings.Contains(body, `<p class="field-error" id="field-Body-error" role="alert">This is not valid JSON at character 9. Check that commas, quotes and brackets are in place.</p>`) {
		t.Errorf("readable error with offset missing:\n%s", body)
	}
	if !strings.Contains(body, `aria-invalid="true" aria-describedby="field-Body-error"`) {
		t.Errorf("textarea not marked invalid and described by its error:\n%s", body)
	}
	if strings.Contains(body, `<div class="alert-error">`) {
		t.Errorf("a JSON error also became a form-level error:\n%s", body)
	}
	var n int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_note`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a refused save wrote %d rows, %v", n, err)
	}
}

func TestJSONWidgetOnEditKeepsTheRowAndTheSubmittedText(t *testing.T) {
	handler, sqlDB := buildJSONNoteAdmin(t)
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "old", `{"ok":true}`)
	submitted := "{\"broken\":\n"
	resp := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", noteBasePath, id), url.Values{"Title": {"new"}, "Body": {submitted}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422:\n%s", resp.Code, body)
	}
	if got := submittedJSONBody(t, body); got != submitted {
		t.Errorf("re-rendered text = %q, want %q", got, submitted)
	}
	var stored string
	if err := sqlDB.QueryRow(`SELECT body FROM admin_note WHERE id = $1`, id).Scan(&stored); err != nil || stored != `{"ok":true}` {
		t.Fatalf("stored body = %q, %v; want it unchanged", stored, err)
	}
}

func TestJSONWidgetCarriesTheFieldHelpAndErrorTogether(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminNote{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminNote{}, admin.Options{
		HelpText: map[string]string{"Body": "Any JSON document."},
		Widgets:  map[string]admin.Widget{"Body": admin.JSON()},
	}); err != nil {
		t.Fatal(err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	body := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Body": {"{"}}).Body.String()
	if !strings.Contains(body, `aria-invalid="true" aria-describedby="field-Body-help field-Body-error"`) {
		t.Errorf("textarea should be described by help then error:\n%s", body)
	}
}

// The script shows the same sentence the server gives, so the markup carries it
// whether or not a catalog is in use.
func TestJSONWidgetCarriesTheInvalidSentenceForTheScript(t *testing.T) {
	handler, _ := buildJSONNoteAdmin(t)
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	want := `data-msg-invalid-detail="This is not valid JSON. Check that commas, quotes and brackets are in place."`
	if !strings.Contains(body, want) {
		t.Errorf("markup missing %q:\n%s", want, body)
	}
}

// Without the shared script the tab list stays hidden, and both panels stay
// visible, stacked. The script hides the inactive panel when it runs.
func TestJSONWidgetWithoutScriptLeavesEveryPanelVisible(t *testing.T) {
	handler, _ := buildJSONNoteAdmin(t)
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	for _, want := range []string{
		`<div class="widget-tabs" role="tablist" aria-label="Body view" data-widget-tabs hidden>`,
		`<div id="field-Body-panel-edit" role="tabpanel" aria-labelledby="field-Body-tab-edit" class="widget-panel">`,
		`<div id="field-Body-panel-view" role="tabpanel" aria-labelledby="field-Body-tab-view" class="widget-panel">`,
		`<button type="button" class="btn-secondary" data-json-apply hidden>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("markup missing %q:\n%s", want, body)
		}
	}
}

func TestJSONWidgetScriptIsServedAtItsHashedURLWithImmutableCaching(t *testing.T) {
	handler, _ := buildJSONNoteAdmin(t)
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	src := regexp.MustCompile(`<script src="(/admin/_assets/widgets\.[0-9a-f]{16}\.js)"`).FindStringSubmatch(body)
	if src == nil {
		t.Fatalf("no widget script src:\n%s", body)
	}

	resp := doRequest(t, handler, http.MethodGet, src[1], nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", src[1], resp.Code)
	}
	if got := resp.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := resp.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	want, err := os.ReadFile("assets/widgets.js")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body.String() != string(want) {
		t.Errorf("served body differs from the embedded file")
	}

	if got := doRequest(t, handler, http.MethodGet, "/admin/_assets/widgets.js", nil).Code; got == http.StatusOK {
		t.Errorf("the unhashed script URL is served")
	}
}
