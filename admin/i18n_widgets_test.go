package admin_test

import (
	"context"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
)

// buildI18NNoteAdmin registers adminNote with widget on Body, the admin behind
// the i18n middleware that resolves resolve's answer.
func buildI18NNoteAdmin(t *testing.T, widget admin.Widget, resolve func(*http.Request) string) http.Handler {
	t.Helper()
	registry := tango.NewRegistry()
	if err := registry.Models().Register(adminNote{}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Admin().Register(adminNote{}, admin.Options{Widgets: map[string]admin.Widget{"Body": widget}}); err != nil {
		t.Fatal(err)
	}
	sqlDB, store := migratedAdminDB(t, registry)
	seedAdminAccountAndSession(t, sqlDB, store)
	var opts []admin.Option
	if resolve != nil {
		opts = append(opts, admin.WithMiddleware(i18n.Middleware(resolve)))
	}
	if err := registry.Register(admin.New(store, opts...)); err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func resolveTo(locale string) func(*http.Request) string {
	return func(*http.Request) string { return locale }
}

func previewHTML(_ context.Context, source string) (template.HTML, error) {
	return template.HTML("<p>" + template.HTMLEscapeString(source) + "</p>"), nil
}

func TestAdminPagesDeclareTheSelectedLanguage(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-lang", map[string]string{"admin.layout.title": "Admin zz"})

	cases := []struct {
		name    string
		resolve func(*http.Request) string
		want    string
	}{
		{"nothing wired", nil, `<html lang="en"`},
		{"a selected catalog", resolveTo("zz-lang"), `<html lang="zz-lang"`},
		{"the registered spelling", resolveTo("ZZ_Lang"), `<html lang="zz-lang"`},
		{"a locale with no catalog", resolveTo("qq-none"), `<html lang="en"`},
	}
	for _, tc := range cases {
		handler := buildI18NNoteAdmin(t, admin.Markdown(), tc.resolve)
		for _, path := range []string{"/admin/login/", noteBasePath, noteBasePath + "new/"} {
			body := doRequest(t, handler, http.MethodGet, path, nil).Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("%s %s: want %q in\n%.300s", tc.name, path, tc.want, body)
			}
		}
	}
}

func TestMarkdownWidgetTranslatesItsLabelsAndScriptMessages(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-md", map[string]string{
		"admin.widget.view_label":            "vista de %s",
		"admin.markdown.tab_write":           "Escribir",
		"admin.markdown.tab_preview":         "Vista previa",
		"admin.markdown.preview_title":       "Vista previa de %s",
		"admin.markdown.count":               "{count} caracteres",
		"admin.markdown.count_max":           "{count} de {max} caracteres",
		"admin.markdown.preview_failed":      "Falló la vista previa.",
		"admin.markdown.preview_unavailable": "Vista previa no disponible. Recarga la página.",
		"admin.markdown.preview_rendering":   "Generando vista previa...",
	})
	handler := buildI18NNoteAdmin(t, admin.Markdown(admin.WithMarkdownPreview(previewHTML)), resolveTo("zz-md"))
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	markup := markdownWidgetMarkup(t, page, "Body")
	if !strings.Contains(page, `title="Vista previa de Body"`) {
		t.Errorf("iframe title not translated:\n%s", page)
	}
	for _, want := range []string{
		`vista de Body`,
		">Escribir</button>", ">Vista previa</button>",
		">0 caracteres</p>",
		`data-msg-count="{count} caracteres"`,
		`data-msg-count-max="{count} de {max} caracteres"`,
		`data-msg-preview-failed="Falló la vista previa."`,
		`data-msg-preview-unavailable="Vista previa no disponible. Recarga la página."`,
		`data-msg-preview-rendering="Generando vista previa..."`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("markup missing %q:\n%s", want, markup)
		}
	}
}

func TestMarkdownWidgetEnglishIsUnchangedWithoutACatalog(t *testing.T) {
	i18ntest.Isolate(t)
	handler := buildI18NNoteAdmin(t, admin.Markdown(admin.WithMarkdownPreview(previewHTML)), nil)
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	for _, want := range []string{">Write</button>", ">Preview</button>", `title="Preview of Body"`, ">0 characters</p>", `aria-label="Body view"`} {
		if !strings.Contains(page, want) {
			t.Errorf("English markup missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "data-msg-") {
		t.Errorf("English markup carries script messages without a locale:\n%s", page)
	}
}

func TestJSONWidgetTranslatesItsLabelsAndScriptMessages(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-json", map[string]string{
		"admin.widget.view_label":   "vista de %s",
		"admin.json.tab_edit":       "Editar",
		"admin.json.tab_formatted":  "Con formato",
		"admin.json.apply":          "Usar este formato",
		"admin.json.status_nothing": "Nada que formatear.",
		"admin.json.status_invalid": "JSON no válido.",
		"admin.json.view_invalid":   "JSON no válido: {error}",
		"admin.json.status_applied": "Formato aplicado.",
	})
	handler := buildI18NNoteAdmin(t, admin.JSON(), resolveTo("zz-json"))
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	for _, want := range []string{
		"vista de Body", ">Editar</button>", ">Con formato</button>", ">Usar este formato</button>",
		`data-msg-nothing="Nada que formatear."`, `data-msg-invalid="JSON no válido."`,
		`data-msg-invalid-detail="JSON no válido: {error}"`, `data-msg-applied="Formato aplicado."`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q:\n%s", want, page)
		}
	}
}

func TestJSONFieldErrorTranslates(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-json-err", map[string]string{
		"admin.error.json_invalid": "JSON no válido",
		"admin.error.json_syntax":  "JSON no válido: %s (en el carácter %d)",
	})
	handler := buildI18NNoteAdmin(t, admin.JSON(), resolveTo("zz-json-err"))
	post := func(body string) string {
		return doRequest(t, handler, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {body}}).Body.String()
	}
	if got := post(`{"a": }`); !strings.Contains(got, "JSON no válido: invalid character &#39;}&#39; looking for beginning of value (en el carácter 7)") {
		t.Errorf("syntax error not translated:\n%s", got)
	}

	english := buildI18NNoteAdmin(t, admin.JSON(), nil)
	got := doRequest(t, english, http.MethodPost, noteBasePath+"new/", url.Values{"Title": {"t"}, "Body": {`{"a": }`}}).Body.String()
	if !strings.Contains(got, "Invalid JSON: invalid character") || !strings.Contains(got, "(at character 7)") {
		t.Errorf("English message changed:\n%s", got)
	}
}

func TestPreviewDocumentIsTranslatedButEndpointErrorsStayEnglish(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-prev", map[string]string{
		"admin.markdown.document_title": "Vista previa",
		"admin.error.csrf":              "falta el token CSRF",
		"admin.error.not_found":         "no encontrado",
	})
	handler := buildI18NNoteAdmin(t, admin.Markdown(admin.WithMarkdownPreview(previewHTML)), resolveTo("zz-prev"))

	ok := previewBody(t, previewRequest(handler, http.MethodPost, notePreviewPath, "hi", testSessionCSRFToken))
	doc, _ := ok["html"].(string)
	if !strings.Contains(doc, `<html lang="zz-prev">`) || !strings.Contains(doc, "<title>Vista previa</title>") {
		t.Errorf("preview document not localised:\n%s", doc)
	}

	if msg := previewBody(t, previewRequest(handler, http.MethodPost, notePreviewPath, "hi", "wrong"))["error"]; msg != "invalid or missing CSRF token" {
		t.Errorf("csrf error = %v, want the English JSON text", msg)
	}
	if msg := previewBody(t, previewRequest(handler, http.MethodPost, "/admin/admin_note/_preview/Nope/", "hi", testSessionCSRFToken))["error"]; msg != "not found" {
		t.Errorf("not found error = %v, want the English JSON text", msg)
	}
	big := strings.Repeat("x", (256<<10)+1)
	if msg := previewBody(t, previewRequest(handler, http.MethodPost, notePreviewPath, big, testSessionCSRFToken))["error"]; msg != "request body too large" {
		t.Errorf("too large error = %v, want the English JSON text", msg)
	}
}
