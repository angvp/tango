# Agent Recipe: OpenAPI documentation

Use this when an app publishes or exports an OpenAPI document for its routes.

Canonical files: `openapi/openapi.go`, `openapi/build.go`, `openapi/schema.go`, `routedoc.go` and `openapi/swaggerui/swaggerui.go`. Human guide: `docs/guides/openapi.md`.

## Rule

- Install `openapi.New(openapi.Options{Title, Version})` in `InstalledApps`; nothing is served otherwise. Add `UI: swaggerui.New()` only when a browsable page is wanted (it adds about 1.7 MB).
- Every included route is listed automatically. Add detail with `tango.Doc(tango.RouteDoc{...})` as a route option: `Summary`, `Body`, `Query`, `Responses` (status to a sample value; `nil` for no body), `Bearer`, `Tags`.
- Pass sample values of the right Go type (`Pet{}`, `[]Pet{}`, `&Pet{}`); the type is what is read. Schemas follow `encoding/json` tags; a pointer or `omitempty` field is optional.
- Hide a route with `RouteDoc{Hidden: true}`, or a path prefix with `Options.ExcludePrefixes`. Pages for browsers (admin, HTML forms) are not API: hide them.
- A document that cannot be built (a func or channel type, a body on GET, a bad status) fails `tango check` and names the route. Run it.
- Export with `tango openapi [-o file]`; the output is byte-stable, so commit it and diff it in CI.
- The routes are public like any others, and global middleware applies to them. If the document should not be public, leave `openapi.New` out of production and publish `tango openapi` output.

Tiny shape:

```go
tango.Path("GET", "/", list, tango.Name("list"), tango.Doc(tango.RouteDoc{
	Summary:   "List pets",
	Query:     PetFilter{},
	Responses: map[int]any{200: []Pet{}},
}))
```

## Don't

- Do not import `internal/` packages or copy the schema reflection; use `tango.Doc`.
- Do not expect the document to follow a handler's code. A `RouteDoc` is a promise: test the route against it.
- Do not describe non-JSON bodies, multiple content types or YAML output; they do not exist.

## Check

- Compare with `openapi/openapi_test.go`, `openapi/schema_test.go` and the golden document in `accounts/testdata/openapi_accounts.golden.json`.
- Read `docs/guides/openapi.md`, and run `docs/agents/checklist.md`.
