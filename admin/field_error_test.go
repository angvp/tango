package admin_test

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

type adminNote struct {
	ID    int64 `tango:"pk"`
	Title string
	Body  string
}

const noteBasePath = "/admin/admin_note/"

// failingWidget is a text input whose Parse always fails with err.
type failingWidget struct{ err error }

func (failingWidget) Render(f admin.FieldContext) template.HTML {
	return template.HTML(fmt.Sprintf(`<input id="field-%s" name="%s" value="%s"%s>`, f.Name, f.Name, template.HTMLEscapeString(f.Value), ""))
}

func (w failingWidget) Parse(admin.FieldContext, admin.FieldValues, reflect.Value) error {
	return w.err
}

func buildNoteAdmin(t *testing.T, options admin.Options) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminNote{}); err != nil {
		t.Fatalf("register model: %v", err)
	}
	if err := registry.Admin().Register(adminNote{}, options); err != nil {
		t.Fatalf("register admin model: %v", err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	if err := registry.Register(admin.New(store)); err != nil {
		t.Fatalf("register admin app: %v", err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatalf("run registration: %v", err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	return handler
}

func TestWidgetFieldErrorRendersBesideItsFieldOnly(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": failingWidget{err: admin.FieldError{Message: "Body is not valid <b>JSON</b>"}},
	}})
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {"x"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.Code, body)
	}
	if !strings.Contains(body, `<p class="field-error" id="field-Body-error" role="alert">Body is not valid &lt;b&gt;JSON&lt;/b&gt;</p>`) {
		t.Errorf("field error missing or unescaped:\n%s", body)
	}
	if strings.Contains(body, `<div class="alert-error">`) {
		t.Errorf("a field error also produced a form-level error:\n%s", body)
	}
	if strings.Contains(body, `id="field-Title-error"`) {
		t.Errorf("the other field got an error:\n%s", body)
	}
	// Title's built-in input stays free of aria attributes.
	if !strings.Contains(body, `<input id="field-Title" type="text" name="Title" value="t" class="input">`) {
		t.Errorf("the valid field's markup changed:\n%s", body)
	}
}

func TestFieldErrorsInsideErrorsJoinAreAllShownAndAcrossFields(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Title": failingWidget{err: errors.Join(admin.FieldError{Message: "first problem"}, fmt.Errorf("wrapped: %w", admin.FieldError{Message: "second problem"}), &admin.FieldError{Message: "third problem"})},
		"Body":  failingWidget{err: admin.FieldError{Message: "body problem"}},
	}})
	body := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}}).Body.String()
	for _, want := range []string{"first problem", "second problem", "third problem", "body problem"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, `id="field-Title-error"`) || !strings.Contains(body, `id="field-Body-error"`) {
		t.Errorf("both fields should have error elements:\n%s", body)
	}
}

func TestPlainParseErrorStaysAFormLevelError(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": failingWidget{err: errors.New("boom")},
	}})
	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `<div class="alert-error">field &#34;Body&#34;: boom</div>`) {
		t.Fatalf("status %d, body:\n%s", resp.Code, body)
	}
	if strings.Contains(body, `<p class="field-error"`) {
		t.Errorf("a plain error became a field error:\n%s", body)
	}
}

func TestFieldErrorOnEditLeavesTheRowUntouched(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminNote{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminNote{}, admin.Options{Widgets: map[string]admin.Widget{
		"Body": failingWidget{err: admin.FieldError{Message: "nope"}},
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
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "old", "old body")

	resp := doRequest(t, handler, http.MethodPost, fmt.Sprintf("%s%d/", noteBasePath, id), url.Values{"Title": {"new"}, "Body": {"x"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `id="field-Body-error"`) || !strings.Contains(body, "nope") {
		t.Fatalf("status %d, body:\n%s", resp.Code, body)
	}
	var title string
	if err := sqlDB.QueryRow(`SELECT title FROM admin_note WHERE id = $1`, id).Scan(&title); err != nil || title != "old" {
		t.Fatalf("title = %q, %v; want the row unchanged", title, err)
	}
}

func TestErroredBuiltInFieldIsMarkedInvalidAndDescribed(t *testing.T) {
	h, _ := buildHeadlineAdmin(t)
	body := doRequest(t, h, http.MethodPost, headlineBasePath+"new/", url.Values{"Title": {"toolong"}}).Body.String()
	want := `<input id="field-Title" type="text" name="Title" value="toolong" maxlength="5" class="input" aria-invalid="true" aria-describedby="field-Title-error">`
	if !strings.Contains(body, want) {
		t.Errorf("errored input lacks aria wiring; want %s in:\n%s", want, body)
	}
	if !strings.Contains(body, `id="field-Title-error"`) {
		t.Errorf("error element has no id:\n%s", body)
	}
	if strings.Contains(body, `<input id="field-Body" type="text" name="Body" value="" class="input" aria`) {
		t.Errorf("an error-free field got aria attributes:\n%s", body)
	}
}

func TestHelpTextRendersAndIsReferencedByTheInput(t *testing.T) {
	handler := buildNoteAdmin(t, admin.Options{
		HelpText: map[string]string{"Title": "Shown <i>everywhere</i>."},
		Widgets:  map[string]admin.Widget{"Body": admin.Textarea()},
	})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	if !strings.Contains(body, `<p class="field-help" id="field-Title-help">Shown &lt;i&gt;everywhere&lt;/i&gt;.</p>`) {
		t.Errorf("help text missing:\n%s", body)
	}
	if !strings.Contains(body, `<input id="field-Title" type="text" name="Title" value="" class="input" aria-describedby="field-Title-help">`) {
		t.Errorf("input does not reference its help text:\n%s", body)
	}
	if strings.Contains(body, `id="field-Body-help"`) {
		t.Errorf("a field without help text got a help element:\n%s", body)
	}
	if !strings.Contains(body, `<textarea id="field-Body" name="Body" class="input" rows="4"></textarea>`) {
		t.Errorf("textarea without help changed:\n%s", body)
	}
}

type adminCapped struct {
	ID   int64  `tango:"pk"`
	Code string `tango:"varchar=3"`
}

func TestHelpTextAndErrorAreBothDescribed(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminCapped{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminCapped{}, admin.Options{HelpText: map[string]string{"Code": "Three letters."}}); err != nil {
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
	body := doRequest(t, handler, http.MethodPost, "/admin/admin_capped/new/", url.Values{"Code": {"abcd"}}).Body.String()
	if !strings.Contains(body, `aria-invalid="true" aria-describedby="field-Code-help field-Code-error"`) {
		t.Errorf("input should be described by help then error:\n%s", body)
	}
}
