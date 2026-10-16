package openapi_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/openapi"
)

type Category struct {
	ID   int64  `json:"id" tango:"pk"`
	Name string `json:"name" tango:"varchar=40"`
}

type Pet struct {
	ID       int64      `json:"id" tango:"pk"`
	Name     string     `json:"name"`
	Category *Category  `json:"category,omitempty"`
	Tags     []string   `json:"tags"`
	Born     time.Time  `json:"born"`
	Deleted  *time.Time `json:"deleted"`
	Weight   float64    `json:"weight,omitempty"`
	Internal string     `json:"-"`
	hidden   string
	Extra    map[string]int `json:"extra,omitempty"`
	Avatar   []byte         `json:"avatar,omitempty"`
	Children []Pet          `json:"children,omitempty"`
}

type petFilter struct {
	Category int64    `query:"category"`
	Status   string   `query:"status,required"`
	Tags     []string `query:"tag"`
	Page     int
	Skip     string `query:"-"`
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

func documentOf(t *testing.T, routes ...tango.Route) map[string]any {
	t.Helper()
	h := serve(t, openapi.Options{}, func(r *tango.Registry) error { return r.Routes().Include("/pets", routes) })
	rec := fetch(t, h, "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	checkDocument(t, rec.Body.Bytes())
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func at(t *testing.T, doc any, path ...string) any {
	t.Helper()
	for _, key := range path {
		m, ok := doc.(map[string]any)
		if !ok {
			t.Fatalf("%v: not an object at %q", path, key)
		}
		doc = m[key]
	}
	return doc
}

func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("got:\n%s\nwant:\n%s", g, w)
	}
}

func TestDocDescribesBodyQueryAndResponses(t *testing.T) {
	doc := documentOf(t, tango.Path("POST", "/", view, tango.Name("create"), tango.Doc(tango.RouteDoc{
		Summary: "Create a pet", Description: "Adds one.", Tags: []string{"shop"},
		Body: Pet{}, Query: petFilter{},
		Responses: map[int]any{201: Pet{}, 400: &apiError{}, 204: nil},
	})))
	op := at(t, doc, "paths", "/pets/", "post").(map[string]any)
	equal(t, op["tags"], []any{"shop"})
	equal(t, op["summary"], "Create a pet")
	equal(t, op["description"], "Adds one.")
	equal(t, op["requestBody"], map[string]any{"required": true, "content": map[string]any{
		"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Pet"}}}})
	equal(t, op["parameters"], []any{
		map[string]any{"name": "category", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "format": "int64"}},
		map[string]any{"name": "status", "in": "query", "required": true, "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "tag", "in": "query", "required": false, "schema": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		map[string]any{"name": "page", "in": "query", "required": false, "schema": map[string]any{"type": "integer", "format": "int64"}},
	})
	equal(t, op["responses"], map[string]any{
		"201": map[string]any{"description": "Created", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Pet"}}}},
		"400": map[string]any{"description": "Bad Request", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/apiError"}}}},
		"204": map[string]any{"description": "No Content"},
	})
}

func TestSchemasAreReflectedFromGoTypes(t *testing.T) {
	doc := documentOf(t, tango.Path("GET", "/{id}", view, tango.Doc(tango.RouteDoc{Responses: map[int]any{200: Pet{}}})))
	schemas := at(t, doc, "components", "schemas").(map[string]any)
	equal(t, schemas["Category"], map[string]any{
		"type": "object", "required": []any{"id", "name"},
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "format": "int64"},
			"name": map[string]any{"type": "string", "maxLength": float64(40)},
		},
	})
	equal(t, schemas["Pet"], map[string]any{
		"type": "object", "required": []any{"id", "name", "tags", "born"},
		"properties": map[string]any{
			"id":   map[string]any{"type": "integer", "format": "int64"},
			"name": map[string]any{"type": "string"},
			"category": map[string]any{"oneOf": []any{
				map[string]any{"$ref": "#/components/schemas/Category"}, map[string]any{"type": "null"}}},
			"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"born":     map[string]any{"type": "string", "format": "date-time"},
			"deleted":  map[string]any{"type": []any{"string", "null"}, "format": "date-time"},
			"weight":   map[string]any{"type": "number", "format": "double"},
			"extra":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "integer", "format": "int64"}},
			"avatar":   map[string]any{"type": "string", "format": "byte"},
			"children": map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Pet"}},
		},
	})
}

func TestUndocumentedAndDocumentedRoutesCoexist(t *testing.T) {
	doc := documentOf(t,
		tango.Path("GET", "/", view, tango.Doc(tango.RouteDoc{Responses: map[int]any{200: []Pet{}}})),
		tango.Path("GET", "/plain", view),
	)
	equal(t, at(t, doc, "paths", "/pets/", "get", "responses", "200", "content", "application/json", "schema"),
		map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/Pet"}})
	equal(t, at(t, doc, "paths", "/pets/plain", "get", "responses"),
		map[string]any{"default": map[string]any{"description": "Response"}})
	if at(t, doc, "paths", "/pets/plain", "get", "requestBody") != nil {
		t.Fatal("an undocumented route has no body schema")
	}
}

func TestUnsupportedDocsFailCheckNamingTheRoute(t *testing.T) {
	cases := map[string]struct {
		doc  tango.RouteDoc
		want string
	}{
		"func in body":     {tango.RouteDoc{Body: struct{ F func() }{}}, "Body"},
		"chan in response": {tango.RouteDoc{Responses: map[int]any{200: make(chan int)}}, "Responses[200]"},
		"nil body sample":  {tango.RouteDoc{Responses: map[int]any{200: nil}}, ""},
		"bad status":       {tango.RouteDoc{Responses: map[int]any{42: nil}}, "42"},
		"query not struct": {tango.RouteDoc{Query: 5}, "Query"},
		"query nested":     {tango.RouteDoc{Query: struct{ P Pet }{}}, "cannot be a struct"},
		"body on GET":      {tango.RouteDoc{Body: Pet{}}, "no body"},
		"map key":          {tango.RouteDoc{Responses: map[int]any{200: map[bool]int{}}}, "keys"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			registry := tango.NewRegistry()
			app := openapi.New(openapi.Options{})
			_ = registry.Register(tango.NewApp("shop", func(r *tango.Registry) error {
				return r.Routes().Include("/pets", []tango.Route{tango.Path("GET", "/x", view, tango.Doc(tc.doc))})
			}))
			_ = registry.Register(app)
			if err := registry.RunRegistration(); err != nil {
				t.Fatal(err)
			}
			checks := app.(tango.Checker).Checks()
			if tc.want == "" {
				if checks[0].Err != nil {
					t.Fatalf("unexpected: %v", checks[0].Err)
				}
				return
			}
			if checks[0].Err == nil || !strings.Contains(checks[0].Err.Error(), tc.want) || !strings.Contains(checks[0].Err.Error(), "/pets/x") {
				t.Fatalf("error %v, want it to name the route and %q", checks[0].Err, tc.want)
			}
		})
	}
}

type namedBody struct {
	N int `json:"n"`
}

func (namedBody) SchemaName() string { return "Named body" }

func TestSchemaNameAndBearerSecurity(t *testing.T) {
	doc := documentOf(t, tango.Path("POST", "/", view, tango.Doc(tango.RouteDoc{
		Bearer: true, Body: namedBody{}, Responses: map[int]any{200: &namedBody{}},
	})))
	equal(t, at(t, doc, "paths", "/pets/", "post", "requestBody", "content", "application/json", "schema"),
		map[string]any{"$ref": "#/components/schemas/Named_body"})
	equal(t, at(t, doc, "paths", "/pets/", "post", "security"), []any{map[string]any{"bearerAuth": []any{}}})
	equal(t, at(t, doc, "components", "securitySchemes"),
		map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}})
}
