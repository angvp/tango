# Content Security Policy and security headers

tanGO sets no site-wide Content-Security-Policy and no site-wide set of security headers until you install `tango.SecurityHeaders`. Individual responses already set some headers, such as `nosniff` on the admin's JSON responses and `Referrer-Policy: no-referrer` on the account reset pages. Once it is installed, every response it wraps carries a policy, a per-request nonce is available to your templates, and the admin, accounts and Swagger UI pages can run under the same policy. Served uploads are the one exception: `storage.Serve` sends a sandbox policy on every object response by default, whether or not the middleware is installed. See [Delivering a file](uploads.md#delivering-a-file).

## Install the strict baseline

```go
config := tango.Config{
	InstalledApps: apps,
	Middleware: []tango.Middleware{
		tango.SecurityHeaders(),
		tango.Recoverer(),
		tango.AccessLogger(),
	},
}
```

Installed with no options, the middleware applies the strict baseline. For a response it wraps, the headers are:

```
Content-Security-Policy: default-src 'self'; script-src 'self' 'nonce-<value>'; style-src 'self' 'nonce-<value>'; object-src 'none'; base-uri 'self'; frame-ancestors 'self'
X-Content-Type-Options: nosniff
Referrer-Policy: strict-origin-when-cross-origin
X-Frame-Options: SAMEORIGIN
```

Under the baseline, a page cannot run inline `<script>` or `<style>` unless the element carries the request's nonce, cannot use inline event-handler attributes or `style=` attributes, and cannot load script, style, images, fonts or frames from another origin. An application that already has inline blocks must add the nonce to them, or move them into files, before it installs the middleware.

Place `SecurityHeaders` outside `Recoverer`, as shown above. A 500 produced by a recovered panic then still carries the policy and the companion headers.

## Write a policy

A policy is a `tango.CSP` value with one field per directive: `DefaultSrc`, `ScriptSrc`, `StyleSrc`, `ImgSrc`, `FontSrc`, `ConnectSrc`, `FormAction`, `ObjectSrc`, `BaseURI` and `FrameAncestors`. Each field is a list of typed sources:

| Constructor | Renders |
| --- | --- |
| `tango.Self()` | `'self'` |
| `tango.None()` | `'none'` |
| `tango.Host(name)` | a host source, such as `cdn.example.com` or `https://cdn.example.com:8443`. It panics on whitespace, quotes, backslashes, commas, semicolons and non-printable characters. |
| `tango.Scheme(s)` | a scheme source, such as `https:`. It panics on an invalid scheme. |
| `tango.Data()` | `data:` |
| `tango.Blob()` | `blob:` |
| `tango.NonceSource()` | `'nonce-…'` for the request (see [Nonces](#nonces)) |
| `tango.UnsafeInlineStyle()` | `'unsafe-inline'`, valid in `style-src` only |

There is no raw-string constructor and no constant for `'unsafe-eval'`, `'unsafe-hashes'` or `'unsafe-inline'` in `script-src`. A misspelled directive or source fails to compile. The package renders the header in one fixed order and omits directives with no sources, so a given policy always produces the same header.

Start from the baseline and change what you need. `tango.StrictCSP()` returns a new value on each call:

```go
policy := tango.StrictCSP()
policy.ImgSrc = []tango.Source{tango.Self(), tango.Host("images.example.com")}
policy.ConnectSrc = []tango.Source{tango.Self(), tango.Host("api.example.com")}

config.Middleware = []tango.Middleware{
	tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)),
	tango.Recoverer(),
}
```

A supplied policy is used exactly as written. It is not merged with the baseline, so a policy that sets only `ScriptSrc` sends only that directive. Passing a policy with no directives panics at startup, because it would send no policy while the host believes one applies.

## Nonces

The baseline's `script-src` and `style-src` contain `NonceSource()`. The middleware generates one nonce per request from 16 bytes of `crypto/rand`, encoded in standard base64, and uses it in the header. It generates a nonce only when the policy uses `NonceSource()` in some directive. Otherwise the request has no nonce.

A View reads the nonce from the request context and passes it to its template:

```go
type pageData struct {
	Nonce string
}

func home(ctx *tango.Context) error {
	return ctx.HTML(http.StatusOK, pages, "home.html", pageData{
		Nonce: tango.CSPNonce(ctx.Context()),
	})
}
```

`CSPNonce` takes a `context.Context`, so raw middleware and views both use it. It returns the empty string when no nonce was generated.

The template renders the attribute with `NonceAttr`. Register it in the template's function map, since the framework does not change your templates:

```go
pages := template.Must(template.New("").Funcs(template.FuncMap{
	"NonceAttr": tango.NonceAttr,
}).ParseFS(templates, "templates/*.html"))
```

```html
<script{{NonceAttr .Nonce}}>
  // runs under the policy
</script>
```

`NonceAttr` returns ` nonce="…"` with a leading space, or nothing when the nonce is empty. A template therefore renders correctly with or without the middleware installed.

If the framework cannot generate a nonce, the middleware answers `500` with `{"error": "internal error"}` and does not call the handler, so a page is never sent without the policy the host configured.

## Tiers and the most specific policy

The middleware attaches at four tiers, outermost first:

1. `tango.Config.Middleware`, for every matched route, or for the whole router with `MiddlewareScopeAll`.
2. `tango.WithMiddleware(...)` on `Routes().Include(...)`, for one group.
3. `tango.Use(...)` on `tango.Path(...)`, for one route.
4. `admin.WithMiddleware(...)`, for the admin's routes (see [The admin](#the-admin)).

The middleware sets its header; it does not add one. Browsers enforce every policy header they receive, and the effects intersect, so an outer header would still restrict a route an inner tier means to relax. For that reason the innermost install's policy is the only one on the response.

- An outer restriction does not carry into an inner tier. A host that wants a restriction inside a group or route repeats it in that tier's policy.
- An inner install generates its own nonce and sets it in the context, so the handler and the header always agree. If the inner policy uses no nonce, `CSPNonce` returns the empty string inside that tier, even when an outer tier generated one.
- The middleware does not wrap the `ResponseWriter`. `http.Hijacker` and `http.Flusher` keep working, so WebSocket upgrades and streaming responses are unaffected.

Under the default scope, `MiddlewareScopeRoutes`, `Config.Middleware` does not wrap unmatched requests. A `404` for a path no route matches carries no headers. Use `MiddlewareScopeAll` when every response, including 404 and 405 pages, must carry the policy.

## Companion headers

| Header | When it is sent | Value |
| --- | --- | --- |
| `X-Content-Type-Options` | Always, once the middleware is installed | `nosniff` |
| `Referrer-Policy` | Always | `strict-origin-when-cross-origin` by default. `tango.WithReferrerPolicy(value)` sets another standard value and panics on a non-standard one. |
| `X-Frame-Options` | When the policy's `FrameAncestors` is absent or `'none'`, or exactly `'self'` | `DENY` for absent or `'none'`, `SAMEORIGIN` for `'self'`. For any other value the header is omitted, because it cannot express the rule. |
| `Strict-Transport-Security` | Only with `tango.WithHSTS(maxAge)`, and only on requests that arrived over TLS | `max-age=<seconds>`, with no `includeSubDomains` and no `preload` |

A handler can override a companion header with its own `Header().Set`. The account password-reset pages, for example, keep `Referrer-Policy: no-referrer`.

The middleware does not read `X-Forwarded-Proto`. Behind a proxy that terminates TLS, set `Strict-Transport-Security` at the proxy, or install `WithHSTS` only where the application sees TLS directly.

This version does not send `Permissions-Policy`, `Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy` or `upgrade-insecure-requests`, and it has no `Content-Security-Policy-Report-Only` mode and no violation report endpoint. Violations are reported only in the browser's console.

## The admin

The admin sends no policy by default. Its pages satisfy `admin.ContentSecurityPolicy()`, which you compose into the admin tier:

```go
admin.New(store, admin.WithMiddleware(
	tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy())),
))
```

For `/admin/` pages this policy replaces any policy the global tier sends. The policy allows the admin's own origin and nothing else: no inline script, no inline style, no inline event handlers, no third-party origins, no framing, and no plugins.

- **Logo on another origin.** If `admin.Branding.LogoURL` is on another origin, add that origin to the policy's image sources before composing it. `admin.ContentSecurityPolicy()` returns a new value on each call, so the change affects only that policy.

  ```go
  policy := admin.ContentSecurityPolicy()
  policy.ImgSrc = append(policy.ImgSrc, tango.Host("assets.example.com"))
  ```

- **Markdown preview.** The preview document is a sandboxed frame with its own meta policy, `default-src 'none'; base-uri 'none'; form-action 'none'`. That meta policy is stricter than the admin's, and the frame inherits the admin's header, so the preview is not relaxed.

- **Host widgets and templates.** The admin's pages no longer depend on inline code, but a widget or template you write does. A strict admin policy blocks an inline `style` attribute and an inline event handler in a widget's markup, and an inline `<script>` without the nonce. Move that behaviour into a file the admin serves, or give the element the nonce from your data.

- **Colour tokens.** The admin's stylesheet is precompiled. Widgets can rely on the colour token utilities for the `text-`, `bg-`, `border-` and `ring-` prefixes, including opacity variants such as `text-ink/60`, listed in the [admin guide](admin-registration.md#light-and-dark-theme). Other Tailwind utilities are not part of that contract and may change in a release, so a widget that needs other styling ships its own stylesheet, served from a content-hashed URL.

## Accounts pages

The account pages use no inline script and no inline style. Their error paragraph takes its colour from a stylesheet the accounts package serves from a content-hashed URL under `/accounts/_assets/`. They request no third-party origin. They work under the baseline with no configuration.

## Swagger UI

Swagger UI needs three relaxations under a strict policy: `style-src` must allow `'unsafe-inline'`, because Swagger UI writes inline styles at runtime; `img-src` must allow `data:`, because its styles reference `data:` images; and `connect-src` must allow `'self'`, so the page can fetch its own document. `swaggerui.WithContentSecurityPolicy` applies those three changes to the UI page's route only, and keeps every other directive of your policy:

```go
policy := tango.StrictCSP()

config.Middleware = []tango.Middleware{
	tango.SecurityHeaders(tango.WithContentSecurityPolicy(policy)),
	tango.Recoverer(),
}

config.InstalledApps = []tango.App{
	openapi.New(openapi.Options{
		UI: swaggerui.New(swaggerui.WithContentSecurityPolicy(policy)),
	}),
	// the other apps
}
```

Pass the same policy value that the middleware uses. The document, the UI's assets and the rest of the application keep the application's policy. The relaxed `style-src` is the one place the framework uses `tango.UnsafeInlineStyle()`, and it is valid only in `style-src`.

## Served uploads

`storage.Serve` sends `Content-Security-Policy: sandbox; default-src 'none'` on every object response unless the route sets `storage.ServeOptions{SkipSandboxPolicy: true}`. The uploads guide describes the policy and the opt-out, and the CHANGELOG records the upgrade note.

## Covered surface

From v0.8.0 the Content-Security-Policy API is part of the [Covered API](../compatibility.md): `tango.SecurityHeaders` with `SecurityHeaderOption`, `WithContentSecurityPolicy`, `WithHSTS` and `WithReferrerPolicy`; the `CSP` type and its directive fields; `StrictCSP`; the `Source` type and its constructors `Self`, `None`, `Host`, `Scheme`, `Data`, `Blob`, `NonceSource` and `UnsafeInlineStyle`; `CSPNonce` and `NonceAttr`; `admin.ContentSecurityPolicy`; `swaggerui.Option`, `swaggerui.WithContentSecurityPolicy` and the variadic `swaggerui.New`; and the `storage.ServeOptions.SkipSandboxPolicy` opt-out. The markup of the admin and Swagger UI pages that a policy governs is not covered.

## Checking a policy

Enable the policy on a staging deployment, then exercise each page in a browser with its developer console open. Each violation names the directive it broke. Fix the page, or loosen the policy for that one directive, before you deploy it.
