package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
)

// buildMarkdownNoteAdmin is buildNoteAdmin that also returns the database,
// so a test can seed rows and read back what a save stored.
func buildMarkdownNoteAdmin(t *testing.T, options admin.Options) (http.Handler, *sql.DB) {
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
	return handler, sqlDB
}

// markdownWidgetMarkup returns the part of an admin page holding one
// Markdown field: from its label through its character count.
func markdownWidgetMarkup(t *testing.T, body, name string) string {
	t.Helper()
	start := strings.Index(body, `<label for="field-`+name+`" class="field-label">`)
	end := strings.Index(body, `id="field-`+name+`-count"`)
	if start < 0 || end < start {
		t.Fatalf("no Markdown widget markup for %s in:\n%s", name, body)
	}
	closing := strings.Index(body[end:], "</p>")
	if closing < 0 {
		t.Fatalf("unterminated count element for %s in:\n%s", name, body)
	}
	return body[start : end+closing+len("</p>")]
}

func bodyOfNote(t *testing.T, sqlDB *sql.DB, id int64) string {
	t.Helper()
	var body string
	if err := sqlDB.QueryRow(`SELECT body FROM admin_note WHERE id = $1`, id).Scan(&body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func TestMarkdownFieldRendersMonospaceResizableTextareaWithCount(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	widget := markdownWidgetMarkup(t, body, "Body")
	for _, want := range []string{
		`<textarea id="field-Body" name="Body"`,
		`font-mono`,
		`resize-y`,
		`data-tango-widget="markdown"`,
		`<p id="field-Body-count"`,
		`0 characters`,
	} {
		if !strings.Contains(widget, want) {
			t.Errorf("Markdown widget missing %q:\n%s", want, widget)
		}
	}
	if strings.Contains(widget, "<script") || strings.Contains(widget, " on") {
		t.Errorf("Markdown widget emitted script or an inline handler:\n%s", widget)
	}
}

func TestMarkdownCountsCharactersNotBytes(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "t", "héllo😀")

	body := doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", noteBasePath, id), nil).Body.String()
	widget := markdownWidgetMarkup(t, body, "Body")
	if !strings.Contains(widget, "6 characters") {
		t.Errorf("count for héllo😀 should be 6 characters:\n%s", widget)
	}
	if !strings.Contains(widget, ">\nhéllo😀</textarea>") {
		t.Errorf("value missing from the textarea:\n%s", widget)
	}
}

// The browser drops one newline right after <textarea>, so the widget writes
// one of its own; a value that itself starts with a newline keeps it.
func TestMarkdownKeepsALeadingNewlineInTheValue(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "t", "\nfirst")

	widget := markdownWidgetMarkup(t, doRequest(t, handler, http.MethodGet, fmt.Sprintf("%s%d/", noteBasePath, id), nil).Body.String(), "Body")
	if !strings.Contains(widget, ">\n\nfirst</textarea>") {
		t.Errorf("leading newline not preserved:\n%s", widget)
	}
}

func TestMarkdownSavesTheSubmittedTextExactly(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	const submitted = "# Heading\r\n\r\n    indented *code*  \n\n- item\n"

	resp := doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {submitted}})
	if resp.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body: %s", resp.Code, resp.Body.String())
	}
	if got := bodyOfNote(t, sqlDB, 1); got != submitted {
		t.Fatalf("stored body = %q, want exactly %q", got, submitted)
	}
}

func TestMarkdownInvalidSaveKeepsTheTextCountAndDescription(t *testing.T) {
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminHeadline{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminHeadline{}, admin.Options{
		Widgets:  map[string]admin.Widget{"Title": admin.Markdown()},
		HelpText: map[string]string{"Title": "Short Markdown"},
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

	// Title is varchar=5, so six characters are refused beside the field.
	resp := doRequest(t, handler, http.MethodPost, headlineBasePath+"new/", url.Values{"Title": {"*abcde"}})
	body := resp.Body.String()
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422:\n%s", resp.Code, body)
	}
	widget := markdownWidgetMarkup(t, body, "Title")
	for _, want := range []string{
		`aria-invalid="true"`,
		`aria-describedby="field-Title-help field-Title-error"`,
		`maxlength="5"`,
		">\n*abcde</textarea>",
		`6 / 5 characters`,
	} {
		if !strings.Contains(widget, want) {
			t.Errorf("invalid Markdown field missing %q:\n%s", want, widget)
		}
	}
	if !strings.Contains(body, `id="field-Title-help"`) || !strings.Contains(body, `id="field-Title-error"`) {
		t.Errorf("help and error elements should both render:\n%s", body)
	}
	if headlineCount(t, sqlDB) != 0 {
		t.Errorf("a refused save stored a row")
	}
}

func TestMarkdownPreviewCallbackIsNeverCalledByTheForm(t *testing.T) {
	calls := 0
	preview := func(context.Context, string) (template.HTML, error) {
		calls++
		return "", nil
	}
	plain, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	withPreview, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown(admin.WithMarkdownPreview(preview))}})

	wantGet := doRequest(t, plain, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	gotGet := doRequest(t, withPreview, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	// The preview adds its own data attribute and tabs; apart from the
	// data-preview-url hook, the textarea and count are unchanged.
	withoutHook := regexp.MustCompile(` data-preview-url="[^"]*"`).ReplaceAllString(gotGet, "")
	fromTextarea := func(markup string) string { return markup[strings.Index(markup, "<textarea"):] }
	if fromTextarea(markdownWidgetMarkup(t, withoutHook, "Body")) != fromTextarea(markdownWidgetMarkup(t, wantGet, "Body")) {
		t.Errorf("a preview callback changed the textarea markup")
	}
	doRequest(t, withPreview, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {"x"}})
	if calls != 0 {
		t.Errorf("preview callback ran %d times on a plain form request, want 0", calls)
	}
	if got := bodyOfNote(t, sqlDB, 1); got != "x" {
		t.Errorf("stored body = %q, want %q", got, "x")
	}
}

func TestMarkdownLeavesOtherFieldsUnchanged(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	body := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	if !strings.Contains(body, `<input id="field-Title" type="text" name="Title" value="" class="input">`) {
		t.Errorf("Title markup changed:\n%s", body)
	}
}
