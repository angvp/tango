package openapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
)

type embeddedBase struct {
	ID int64 `json:"id"`
}

type withEmbedded struct {
	embeddedBase
	*Category
	Name  string `json:"name"`
	Skip  string `json:"-"`
	Maybe *int   `json:"maybe"`
}

type unsupported struct {
	C chan int `json:"c"`
}

type badMapKey struct {
	M map[float64]string `json:"m"`
}

type goodMaps struct {
	ByID   map[int64]string    `json:"by_id"`
	ByName map[string][]string `json:"by_name"`
	Fixed  [3]string           `json:"fixed"`
	Opaque []byte              `json:"opaque"`
	Any    any                 `json:"any"`
	Small  int8                `json:"small"`
	Big    uint64              `json:"big"`
	Ratio  float32             `json:"ratio"`
}

type pointerRef struct {
	Owner *Category `json:"owner"`
	Count *int64    `json:"count"`
}

// generic round-trips a document through JSON, so tests read it as a client would.
func generic(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func documentFor(t *testing.T, docs tango.RouteDoc) (map[string]any, error) {
	t.Helper()
	return openapi.Document([]tango.DescribedRoute{{Method: "POST", Pattern: "/x", Doc: &docs}}, openapi.Options{})
}

func TestSchemasCoverEmbeddedPointerMapAndScalarShapes(t *testing.T) {
	doc, err := documentFor(t, tango.RouteDoc{
		Body:      &withEmbedded{},
		Responses: map[int]any{200: goodMaps{}, 201: pointerRef{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	schemas := generic(t, doc)["components"].(map[string]any)["schemas"].(map[string]any)

	embedded := schemas["withEmbedded"].(map[string]any)["properties"].(map[string]any)
	for _, name := range []string{"id", "name", "maybe"} {
		if _, ok := embedded[name]; !ok {
			t.Errorf("withEmbedded is missing %q: %v", name, embedded)
		}
	}
	if _, ok := embedded["Skip"]; ok {
		t.Error(`a json:"-" field must be left out`)
	}

	maps := schemas["goodMaps"].(map[string]any)["properties"].(map[string]any)
	if got := maps["by_id"].(map[string]any)["type"]; got != "object" {
		t.Errorf("by_id type = %v", got)
	}
	if got := maps["fixed"].(map[string]any)["type"]; got != "array" {
		t.Errorf("fixed type = %v", got)
	}
	if got := maps["opaque"].(map[string]any)["format"]; got != "byte" {
		t.Errorf("opaque format = %v", got)
	}
	if got := maps["small"].(map[string]any)["format"]; got != "int32" {
		t.Errorf("small format = %v", got)
	}
	if got := maps["big"].(map[string]any)["format"]; got != "int64" {
		t.Errorf("big format = %v", got)
	}
	if got := maps["ratio"].(map[string]any)["format"]; got != "float" {
		t.Errorf("ratio format = %v", got)
	}

	refs := schemas["pointerRef"].(map[string]any)["properties"].(map[string]any)
	if _, ok := refs["owner"].(map[string]any)["oneOf"]; !ok {
		t.Errorf("a pointer to a named struct must allow null: %v", refs["owner"])
	}
	if got := refs["count"].(map[string]any)["type"].([]any); len(got) != 2 {
		t.Errorf("a pointer scalar must allow null: %v", got)
	}
}

func TestUndescribableTypesAreRejected(t *testing.T) {
	cases := map[string]tango.RouteDoc{
		"channel field":      {Body: unsupported{}},
		"float map key":      {Responses: map[int]any{200: badMapKey{}}},
		"query non-struct":   {Query: 5},
		"query struct field": {Query: struct{ Nested Category }{}},
		"query bad field":    {Query: struct{ C chan int }{}},
	}
	for name, docs := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := documentFor(t, docs); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestOperationIDsAreUniqueAndUnknownMethodsAreSkipped(t *testing.T) {
	routes := []tango.DescribedRoute{
		{Method: "GET", Pattern: "/a", Name: "list"},
		{Method: "GET", Pattern: "/b", Name: "list"},
		{Method: "TRACE", Pattern: "/t"},
		{Method: "BOGUS", Pattern: "/skipped"},
	}
	doc, err := openapi.Document(routes, openapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	paths := generic(t, doc)["paths"].(map[string]any)
	if _, ok := paths["/skipped"]; ok {
		t.Error("a route with an unknown method must be left out")
	}
	first := paths["/a"].(map[string]any)["get"].(map[string]any)["operationId"]
	second := paths["/b"].(map[string]any)["get"].(map[string]any)["operationId"]
	if first != "list" || second != "list_2" {
		t.Errorf("operationIds = %v, %v; want list, list_2", first, second)
	}
	if _, ok := paths["/t"].(map[string]any)["trace"]; !ok {
		t.Error("TRACE must be listed")
	}
}

func TestTheSameRouteTwiceIsAnError(t *testing.T) {
	routes := []tango.DescribedRoute{{Method: "GET", Pattern: "/a"}, {Method: "GET", Pattern: "/a"}}
	if _, err := openapi.Document(routes, openapi.Options{}); err == nil || !strings.Contains(err.Error(), "registered twice") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenAPIAndChecksNeedRegistrationAndReportErrors(t *testing.T) {
	app := openapi.New(openapi.Options{})
	provider, ok := app.(interface {
		OpenAPI() ([]byte, error)
		Checks() []tango.AppCheck
	})
	if !ok {
		t.Fatal("the app must expose OpenAPI and Checks")
	}
	if _, err := provider.OpenAPI(); err == nil {
		t.Error("OpenAPI before registration must fail")
	}
	if checks := provider.Checks(); checks != nil {
		t.Errorf("Checks before registration = %v, want nil", checks)
	}

	bad := tango.NewApp("bad", func(r *tango.Registry) error {
		return r.Routes().Include("/b", []tango.Route{
			tango.Path("POST", "/", view, tango.Doc(tango.RouteDoc{Body: unsupported{}})),
		})
	})
	registry := tango.NewRegistry()
	for _, a := range []tango.App{bad, app} {
		if err := registry.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.OpenAPI(); err == nil {
		t.Error("OpenAPI must report a route that cannot be described")
	}
	checks := provider.Checks()
	if len(checks) != 1 || checks[0].Err == nil {
		t.Fatalf("Checks = %+v, want one failing check", checks)
	}
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	if rec := fetch(t, handler, "/openapi.json"); rec.Code != http.StatusInternalServerError {
		t.Errorf("serving an undescribable document = %d, want 500", rec.Code)
	}
}

type thingA struct {
	A int `json:"a"`
}

func (thingA) SchemaName() string { return "Thing" }

type thingB struct {
	B int `json:"b"`
}

func (thingB) SchemaName() string { return "Thing" }

type thingC struct {
	C int `json:"c"`
}

func (thingC) SchemaName() string { return "Thing" }

func TestSchemaNameCollisionsAreQualifiedByPackage(t *testing.T) {
	doc, err := documentFor(t, tango.RouteDoc{Responses: map[int]any{200: thingA{}, 201: thingB{}, 202: thingC{}}})
	if err != nil {
		t.Fatal(err)
	}
	schemas := generic(t, doc)["components"].(map[string]any)["schemas"].(map[string]any)
	if len(schemas) != 3 {
		t.Fatalf("schemas = %v, want three distinct names", schemas)
	}
	if _, ok := schemas["Thing"]; !ok {
		t.Errorf("the first type keeps its name: %v", schemas)
	}
	for name := range schemas {
		if name != "Thing" && !strings.HasPrefix(name, "openapi_test_Thing") {
			t.Errorf("collision name %q is not qualified by its package", name)
		}
	}
}

func TestBadVarcharAndUnsupportedElementsAreHandled(t *testing.T) {
	if _, err := documentFor(t, tango.RouteDoc{Body: struct {
		Name string `json:"name" tango:"varchar=oops"`
	}{}}); err != nil {
		t.Fatalf("an unreadable varchar size is ignored, got %v", err)
	}
	if _, err := documentFor(t, tango.RouteDoc{Body: []unsupported{}}); err == nil {
		t.Error("a slice of an undescribable type must fail")
	}
}
