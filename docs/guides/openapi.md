# OpenAPI documentation

The `openapi` package describes an app's HTTP routes as an [OpenAPI 3.1](https://spec.openapis.org/oas/v3.1.0) document, served by the running app, exported by `tango openapi`, and optionally browsable in an embedded Swagger UI. It is built from the routes your apps already registered, so it cannot list a route that does not exist or miss one that does.

Nothing is served unless you install it.

## Install it

Add `openapi.New` to `InstalledApps`. Order does not matter: the document is built on the first request, after every route is registered.

```go
import (
	"github.com/angvp/tango/openapi"
	"github.com/angvp/tango/openapi/swaggerui"
)

config.InstalledApps = []tango.App{
	shop.App,
	openapi.New(openapi.Options{
		Title:   "Pet shop",
		Version: "1.0.0",
		UI:      swaggerui.New(), // optional: a browsable page at /docs/
	}),
	admin.New(store), // admin.New stays last
}
```

`/openapi.json` now answers with the document, and `/docs/` with Swagger UI. `Options` also has `Description`, `Servers`, `Path` (default `/openapi.json`), `UIPath` (default `/docs/`) and `ExcludePrefixes`.

### Only in development

`openapi.Dev` takes the same `Options` but serves the document and the UI only when the `TANGO_OPENAPI` environment variable is true (`1`, `true`). Unset, or any other value, and both paths answer 404, so a production process that does not set it exposes nothing.

```sh
TANGO_OPENAPI=1 go run . runserver
```

`tango openapi` and `tango check` work whether or not the variable is set, because they read the route table and serve nothing. `swaggerui` is still linked into the binary (about 1.7 MB) when you import it.

The two routes are ordinary routes, so global `Config.Middleware` applies to them like any other. There is no way to put different middleware on just these routes. If the document should not be public, use `openapi.Dev` (above), or leave the app out of the production `InstalledApps`, and publish the output of `tango openapi` instead.

## What is listed without any work

Every included route appears with its HTTP method, its path, one required string path parameter for each `{placeholder}` (a regular expression such as `{id:[0-9]+}` is dropped and a trailing `*` becomes `{path}`), an operation id (the route's name, or the method and path), and a tag taken from the first segment of the prefix it was included under. A route with no further detail answers `default: Response`.

Routes using other methods than GET, PUT, POST, DELETE, OPTIONS, HEAD, PATCH and TRACE are skipped. The document's own routes, the Swagger UI's, the admin's pages and the accounts cookie-flow pages are never listed: they are for browsers. Leave out more with `ExcludePrefixes: []string{"/internal/"}` or `tango.Doc(tango.RouteDoc{Hidden: true})` on one route.

## Describe a route with `tango.Doc`

<!-- snippet: openapi-doc -->
```go
tango.Path("POST", "/", create, tango.Name("create"), tango.Doc(tango.RouteDoc{
	Summary:   "Create a pet",
	Body:      Pet{},
	Query:     PetFilter{},
	Responses: map[int]any{201: Pet{}, 400: apiError{}, 204: nil},
	Bearer:    true,
}))
```

Pass a zero value or a pointer to one; only its Go type is read. A `nil` response has no body.

| `RouteDoc` field | Becomes |
|---|---|
| `Summary`, `Description`, `Tags`, `Deprecated` | the same on the operation; `Tags` replaces the prefix tag |
| `Body` | a required `application/json` request body (not allowed on GET, HEAD or TRACE) |
| `Query` | a struct whose fields are query parameters, named by `query:"name"` (default: the lower-cased Go name), required with `query:"name,required"`, left out with `query:"-"` |
| `Responses` | one response per status, `application/json` when the value is not nil |
| `Bearer` | `security: bearerAuth` and an HTTP bearer scheme under `components/securitySchemes` |
| `Hidden` | the route is left out |

### How Go types become schemas

Schemas follow `encoding/json`. A field is named by its `json` tag, left out by `json:"-"`, optional when it is a pointer or its tag says `omitempty` or `omitzero`, and required otherwise. Named structs are described once under `components/schemas` and referenced; recursive types work. Slices and arrays are `array`; `map[string]T` is an object with `additionalProperties`; `time.Time` is a `date-time` string; `[]byte` is a `byte` string; `json.RawMessage` and `any` accept anything; a pointer allows `null`. `tango:"varchar=n"` becomes `maxLength`.

A type picks its schema's name with a `SchemaName() string` method; otherwise the Go type name is used, qualified by its package when two types share a name.

Types with no JSON form (functions, channels), maps whose keys are not strings or integers, a struct used as a query parameter, a body on GET, or a status outside 100 to 599 cannot be described. `tango check` fails and names the route.

## tanGO's own endpoints

With `accounts.WithJSON`, the six `/accounts/api/` endpoints appear with their request and response schemas, the `{error, code, fields?}` error shape and the bearer scheme, under the `accounts` tag. The admin has no JSON API and is not listed.

## Export the document

```sh
tango openapi                 # print it
tango openapi -o openapi.json # write it; a failed run leaves an existing file alone
```

The project's own binary produces it (`go run . -tango-openapi`), and the output is the same bytes on every run, so it can be committed and diffed in CI, or fed to a client generator. It needs `openapi.New` or `openapi.Dev` in `InstalledApps`; without it the command says so.

## Swagger UI

`swaggerui.New()` serves Swagger UI 5.17.14 from the binary: no network request, and no inline script, so a Content-Security-Policy that allows same-origin scripts works (Swagger UI sets inline styles, so `style-src` needs `'unsafe-inline'`). Its Authorize button takes a bearer token for routes marked `Bearer`. Importing the package adds about 1.7 MB to the binary, which is why it is separate: an app that only wants the document leaves it out.

## Limits

The document describes what you tell it. A handler's real behaviour is not read from its code, so a `RouteDoc` can drift from it: keep a test that calls the route and compares (see the accounts tests for one). Only JSON bodies are described; there is no YAML, no request validation from the document, and no generated CRUD endpoints.
