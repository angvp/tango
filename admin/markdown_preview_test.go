package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango/admin"
)

// notePreviewPath is the derived preview route for adminNote's Body field.
const notePreviewPath = "/admin/admin_note/_preview/Body/"

// previewCSP is the exact Content-Security-Policy meta the preview document
// carries.
const previewCSP = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; base-uri 'none'; form-action 'none'">`

// previewRequest sends source to path as the Markdown preview's client does:
// a text/plain body and the CSRF token in X-CSRF-Token, with the session
// cookie the other admin tests use.
func previewRequest(handler http.Handler, method, path, source, csrf string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(source))
	request.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// previewBody decodes a preview response and returns its JSON object.
func previewBody(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("preview body is not a JSON object: %v\n%s", err, response.Body.String())
	}
	return body
}

func markdownPreviewHandler(t *testing.T, preview admin.MarkdownPreview) http.Handler {
	t.Helper()
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": admin.Markdown(admin.WithMarkdownPreview(preview)),
	}})
	return handler
}

func TestPreviewRouteReturnsTheHostHTMLInAMinimalCSPDocument(t *testing.T) {
	var got string
	handler := markdownPreviewHandler(t, func(_ context.Context, source string) (template.HTML, error) {
		got = source
		return "<p>hello <em>there</em></p>", nil
	})

	response := previewRequest(handler, http.MethodPost, notePreviewPath, "# Hi", testSessionCSRFToken)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", response.Code, response.Body.String())
	}
	if got != "# Hi" {
		t.Fatalf("renderer received %q, want the submitted source", got)
	}
	if ct := response.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", response.Header().Get("X-Content-Type-Options"))
	}

	html, ok := previewBody(t, response)["html"].(string)
	if !ok {
		t.Fatalf("response has no html string: %s", response.Body.String())
	}
	for _, want := range []string{
		"<!doctype html>",
		`<meta charset="utf-8">`,
		previewCSP,
		"<body>\n<p>hello <em>there</em></p>\n</body>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("preview document missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script") {
		t.Errorf("preview document must not add a script:\n%s", html)
	}
}

func TestPreviewAcceptsTheSourceAtTheCapAndRefusesOneByteMore(t *testing.T) {
	const limit = 256 << 10
	var seen int
	handler := markdownPreviewHandler(t, func(_ context.Context, source string) (template.HTML, error) {
		seen = len(source)
		return "ok", nil
	})

	atCap := previewRequest(handler, http.MethodPost, notePreviewPath, strings.Repeat("a", limit), testSessionCSRFToken)
	if atCap.Code != http.StatusOK {
		t.Fatalf("source at the cap: status = %d, want 200; body: %s", atCap.Code, atCap.Body.String())
	}
	if seen != limit {
		t.Fatalf("renderer received %d bytes, want %d", seen, limit)
	}

	over := previewRequest(handler, http.MethodPost, notePreviewPath, strings.Repeat("a", limit+1), testSessionCSRFToken)
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("source one byte over the cap: status = %d, want 413", over.Code)
	}
	if got := previewBody(t, over)["error"]; got != "request body too large" {
		t.Fatalf("error = %v, want \"request body too large\"", got)
	}
}

func TestPreviewRendererErrorIsAUserSafeJSONErrorAndLeaksNoDetail(t *testing.T) {
	handler := markdownPreviewHandler(t, func(context.Context, string) (template.HTML, error) {
		return "", errors.New("pq: relation secret_table does not exist")
	})

	response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
	body := previewBody(t, response)
	if _, ok := body["html"]; ok {
		t.Fatalf("an error response carries html: %s", response.Body.String())
	}
	message, _ := body["error"].(string)
	if message == "" || strings.Contains(response.Body.String(), "secret_table") {
		t.Fatalf("error = %q, want a short message without the renderer's detail", message)
	}
}

func TestPreviewOutputOverOneMiBIsRejectedAsJSON(t *testing.T) {
	handler := markdownPreviewHandler(t, func(context.Context, string) (template.HTML, error) {
		return template.HTML(strings.Repeat("x", 1<<20+1)), nil
	})

	response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.Code)
	}
	if _, ok := previewBody(t, response)["error"]; !ok {
		t.Fatalf("oversized output should answer with an error object: %s", response.Body.String()[:min(200, response.Body.Len())])
	}
}

func TestPreviewRendererGetsAFiveSecondDeadline(t *testing.T) {
	var remaining time.Duration
	handler := markdownPreviewHandler(t, func(ctx context.Context, _ string) (template.HTML, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return "", errors.New("no deadline")
		}
		remaining = time.Until(deadline)
		return "ok", nil
	})

	if response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if remaining < 4*time.Second || remaining > 5*time.Second {
		t.Fatalf("renderer deadline in %v, want about five seconds", remaining)
	}
}

func TestPreviewRendererThatHonoursCancellationReturnsPromptly(t *testing.T) {
	restore := admin.SetPreviewTimeoutForTest(50 * time.Millisecond)
	defer restore()
	handler := markdownPreviewHandler(t, func(ctx context.Context, _ string) (template.HTML, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	started := time.Now()
	response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken)
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", response.Code)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("a cancelled renderer held the request for %v", elapsed)
	}
}

// neverCalled is a preview callback that fails the test if it runs.
func neverCalled(t *testing.T) admin.MarkdownPreview {
	t.Helper()
	return func(context.Context, string) (template.HTML, error) {
		t.Errorf("the preview callback ran for a request that should not reach it")
		return "", nil
	}
}

func TestPreviewRouteIsPostOnly(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	request := httptest.NewRequest(http.MethodGet, notePreviewPath, nil)
	request.AddCookie(&http.Cookie{Name: "tango_admin_session", Value: testSessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", response.Code)
	}
}

func TestPreviewUnknownOrNonPreviewFieldsAre404(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	for _, path := range []string{
		"/admin/admin_note/_preview/Nope/",
		"/admin/admin_note/_preview/Title/",
	} {
		t.Run(path, func(t *testing.T) {
			response := previewRequest(handler, http.MethodPost, path, "x", testSessionCSRFToken)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body: %s", response.Code, response.Body.String())
			}
			if _, ok := previewBody(t, response)["error"]; !ok {
				t.Fatalf("404 should be a JSON error: %s", response.Body.String())
			}
		})
	}
}

func TestPreviewUnknownModelIsNotFound(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	response := previewRequest(handler, http.MethodPost, "/admin/nope_model/_preview/Body/", "x", testSessionCSRFToken)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestPreviewReadOnlyPreviewFieldIs404(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{
		ReadOnly: []string{"Body"},
		Widgets:  map[string]admin.Widget{"Body": admin.Markdown(admin.WithMarkdownPreview(neverCalled(t)))},
	})
	response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestPreviewWithoutAValidCSRFTokenIs403(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	for name, token := range map[string]string{"missing": "", "wrong": "not-the-token"} {
		t.Run(name, func(t *testing.T) {
			response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", token)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.Code)
			}
			if _, ok := previewBody(t, response)["error"]; !ok {
				t.Fatalf("403 should be a JSON error: %s", response.Body.String())
			}
		})
	}
}

func TestPreviewRequiresTheSameLoginAsOtherAdminRoutes(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))

	anonymous := httptest.NewRequest(http.MethodPost, notePreviewPath, strings.NewReader("x"))
	anonymous.Header.Set("X-CSRF-Token", testSessionCSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, anonymous)
	if response.Code != http.StatusFound || !strings.HasPrefix(response.Header().Get("Location"), "/admin/login/") {
		t.Fatalf("anonymous status = %d, Location = %q; want a redirect to login", response.Code, response.Header().Get("Location"))
	}
}

func TestPreviewRefusesANonStaffAccount(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": admin.Markdown(admin.WithMarkdownPreview(neverCalled(t))),
	}})
	if _, err := sqlDB.Exec(`UPDATE admin_user SET is_staff = FALSE WHERE username = 'admin'`); err != nil {
		t.Fatalf("demote account: %v", err)
	}
	response := previewRequest(handler, http.MethodPost, notePreviewPath, "x", testSessionCSRFToken)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
}

func TestPreviewNeverPersistsAnything(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": admin.Markdown(admin.WithMarkdownPreview(func(context.Context, string) (template.HTML, error) {
			return "<p>preview</p>", nil
		})),
	}})
	id := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "t", "stored")
	var before int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_note`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	if response := previewRequest(handler, http.MethodPost, notePreviewPath, "changed", testSessionCSRFToken); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var after int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM admin_note`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("row count changed from %d to %d", before, after)
	}
	if got := bodyOfNote(t, sqlDB, id); got != "stored" {
		t.Fatalf("stored body = %q, want it unchanged", got)
	}
}

func previewWidgetMarkup(t *testing.T, body string) string {
	t.Helper()
	start := strings.Index(body, `<label for="field-Body" class="field-label">`)
	end := strings.Index(body, `<iframe id="field-Body-preview"`)
	if start < 0 || end < start {
		t.Fatalf("no preview controls for Body in:\n%s", body)
	}
	closing := strings.Index(body[end:], "</iframe>")
	if closing < 0 {
		t.Fatalf("unterminated preview iframe in:\n%s", body)
	}
	return body[start : end+closing+len("</iframe>")]
}

func TestPreviewWidgetRendersHiddenControlsAndASandboxedFrame(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	widget := previewWidgetMarkup(t, page)

	for _, want := range []string{
		`data-preview-url="/admin/admin_note/_preview/Body/"`,
		`<div class="widget-tabs" role="tablist" aria-label="Body view" data-widget-tabs hidden>`,
		`<button type="button" role="tab" id="field-Body-tab-write" class="widget-tab" aria-selected="true" aria-controls="field-Body-panel-write" data-tab="write">Write</button>`,
		`<button type="button" role="tab" id="field-Body-tab-preview" class="widget-tab" aria-selected="false" aria-controls="field-Body-panel-preview" tabindex="-1" data-tab="preview">Preview</button>`,
		`<div id="field-Body-panel-write" role="tabpanel" aria-labelledby="field-Body-tab-write" class="widget-panel">`,
		`<div id="field-Body-panel-preview" role="tabpanel" aria-labelledby="field-Body-tab-preview" class="widget-panel" hidden>`,
		`<p id="field-Body-preview-status" role="status"`,
		`<iframe id="field-Body-preview" title="Preview of Body" sandbox="" referrerpolicy="no-referrer"`,
	} {
		if !strings.Contains(widget, want) {
			t.Errorf("preview widget missing %q:\n%s", want, widget)
		}
	}
	if strings.Contains(widget, "data-markdown-preview") {
		t.Errorf("the Preview button is replaced by the Preview tab:\n%s", widget)
	}
	frame := widget[strings.Index(widget, "<iframe"):]
	for _, forbidden := range []string{"allow-", "srcdoc", "src="} {
		if strings.Contains(frame, forbidden) {
			t.Errorf("iframe must not carry %q:\n%s", forbidden, frame)
		}
	}
	if strings.Contains(widget, "<script") {
		t.Errorf("widget markup must not carry an inline or per-widget script:\n%s", widget)
	}
}

func TestPlainMarkdownWidgetHasNoPreviewControls(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	for _, forbidden := range []string{"_preview", "sandbox", "data-markdown-preview", "<iframe"} {
		if strings.Contains(page, forbidden) {
			t.Errorf("a Markdown widget without a callback must not render %q", forbidden)
		}
	}
}

func TestMarkdownPreviewFormPageLoadsTheSharedWidgetScriptOnce(t *testing.T) {
	handler, _ := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{"Body": admin.Markdown()}})
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()
	if n := strings.Count(page, `<script src="/admin/_assets/widgets.`); n != 1 {
		t.Fatalf("form page loads the widget script %d times, want once:\n%s", n, page)
	}
}
