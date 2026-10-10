package admin_test

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango/admin"
)

var (
	previewURLPattern = regexp.MustCompile(`data-preview-url="([^"]*)"`)
	assetScriptTag    = regexp.MustCompile(`<script src="(/admin/_assets/widgets\.[0-9a-f]{16}\.js)" defer></script>`)
)

// The preview URL the page renders must reach the one registered preview
// route from the create page and from an edit page alike.
func TestMarkdownPreviewURLReachesTheRegisteredRouteFromCreateAndEditPages(t *testing.T) {
	handler, sqlDB := buildMarkdownNoteAdmin(t, admin.Options{Widgets: map[string]admin.Widget{
		"Body": admin.Markdown(admin.WithMarkdownPreview(func(_ context.Context, source string) (template.HTML, error) {
			return template.HTML("<p>" + template.HTMLEscapeString(source) + "</p>"), nil
		})),
	}})
	editID := insertReturningID(t, sqlDB, `INSERT INTO admin_note (title, body) VALUES ($1, $2) RETURNING id`, "t", "body")

	for _, pagePath := range []string{noteBasePath + "new/", fmt.Sprintf("%s%d/", noteBasePath, editID)} {
		page := doRequest(t, handler, http.MethodGet, pagePath, nil).Body.String()
		match := previewURLPattern.FindStringSubmatch(page)
		if match == nil {
			t.Fatalf("%s: no data-preview-url in page", pagePath)
		}
		if match[1] != noteBasePath+"_preview/Body/" {
			t.Errorf("%s: preview URL = %q, want the absolute canonical %q", pagePath, match[1], noteBasePath+"_preview/Body/")
		}
		base, _ := url.Parse(pagePath)
		target, err := base.Parse(match[1])
		if err != nil {
			t.Fatalf("%s: unparsable preview URL %q: %v", pagePath, match[1], err)
		}
		if want := noteBasePath + "_preview/Body/"; target.Path != want {
			t.Errorf("%s: preview URL %q resolves to %q, want %q", pagePath, match[1], target.Path, want)
		}
		response := previewRequest(handler, http.MethodPost, target.Path, "hi", testSessionCSRFToken)
		if response.Code != http.StatusOK {
			t.Errorf("%s: POST to resolved preview URL = %d, want 200: %s", pagePath, response.Code, response.Body.String())
		}
	}
}

// A page whose only behaviour widget is Markdown loads the shared hashed
// asset itself, and the widget markup works as a plain textarea without it.
func TestMarkdownOnlyPageLoadsTheHashedWidgetAsset(t *testing.T) {
	handler := markdownPreviewHandler(t, neverCalled(t))
	page := doRequest(t, handler, http.MethodGet, noteBasePath+"new/", nil).Body.String()

	scripts := assetScriptTag.FindAllStringSubmatch(page, -1)
	if len(scripts) == 0 {
		t.Fatalf("no hashed widgets.js script on a Markdown-only page:\n%s", page)
	}
	asset := doRequest(t, handler, http.MethodGet, scripts[0][1], nil)
	if asset.Code != http.StatusOK || !strings.Contains(asset.Header().Get("Content-Type"), "javascript") {
		t.Errorf("asset %s = %d %q, want 200 JavaScript", scripts[0][1], asset.Code, asset.Header().Get("Content-Type"))
	}
	widget := previewWidgetMarkup(t, page)
	if strings.Contains(widget, "<script") || strings.Contains(widget, " onclick") {
		t.Errorf("widget markup carries script or inline handlers:\n%s", widget)
	}
	if !strings.Contains(page, `<textarea id="field-Body" name="Body"`) || !strings.Contains(widget, "data-widget-tabs hidden") {
		t.Errorf("without JavaScript the field must stay a textarea with hidden tabs:\n%s", widget)
	}
}
