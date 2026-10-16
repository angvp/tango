package openapi_test

// The `tango.Doc` snippet in docs/guides/openapi.md, the block after the
// `<!-- snippet: openapi-doc -->` marker, must appear (ignoring whitespace) in
// this file, and this file runs it.

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
)

func create(ctx *tango.Context) error { return ctx.JSON(http.StatusCreated, Pet{}) }

type PetFilter = petFilter

func guideRoutes() tango.Route {
	return tango.Path("POST", "/", create, tango.Name("create"), tango.Doc(tango.RouteDoc{
		Summary:   "Create a pet",
		Body:      Pet{},
		Query:     PetFilter{},
		Responses: map[int]any{201: Pet{}, 400: apiError{}, 204: nil},
		Bearer:    true,
	}))
}

func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func TestGuideSnippetIsThisFilesCode(t *testing.T) {
	guide, err := os.ReadFile("../docs/guides/openapi.md")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile("(?s)<!-- snippet: openapi-doc -->\n```go\n(.*?)\n```").FindSubmatch(guide)
	if match == nil {
		t.Fatal("the guide has no `<!-- snippet: openapi-doc -->` block")
	}
	self, err := os.ReadFile("guide_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(squash(string(self)), squash(string(match[1]))) {
		t.Fatalf("the guide's snippet is not in guide_test.go; update one to match the other:\n%s", match[1])
	}
}

func TestGuideSnippetBuildsADocument(t *testing.T) {
	h := serve(t, openapi.Options{}, func(r *tango.Registry) error {
		return r.Routes().Include("/pets", []tango.Route{guideRoutes()})
	})
	rec := fetch(t, h, "/openapi.json")
	checkDocument(t, rec.Body.Bytes())
	if !strings.Contains(rec.Body.String(), `"bearerAuth"`) || !strings.Contains(rec.Body.String(), `"Create a pet"`) {
		t.Fatalf("the guide's route is not described:\n%s", rec.Body)
	}
}
