# Agent Recipe: Content Security Policy and Security Headers

Use this when adding security headers, a Content-Security-Policy, a nonce for an inline script or style, or when running the admin or the Swagger UI page under a policy.

Canonical files: `security_headers.go`, `admin/content_security_policy.go`, `openapi/swaggerui/swaggerui.go` and `storage/serve.go`. Runnable proof: `examples/api-with-admin`, whose main.go installs the policy and whose tests check the headers. Human guide: `docs/guides/content-security-policy.md`.

## Rule

- The middleware is opt-in: `tango.SecurityHeaders(...)` in `Config.Middleware`, placed outside `tango.Recoverer()`, or at a narrower tier. With no options it applies the strict baseline, so an inline script or style needs the request's nonce.
- A policy is a typed `tango.CSP` built from `tango.Source` constructors. Never assemble a header string. A supplied policy is used as written; it is not merged with `tango.StrictCSP()`.
- Only the innermost install's header is sent. A restriction on an outer tier does not carry into an inner tier; repeat it in the inner policy.
- A template gets the nonce from its view data: `Nonce: tango.CSPNonce(ctx.Context())`, rendered as `<script{{NonceAttr .Nonce}}>` with `tango.NonceAttr` in the template's function map. The nonce exists only when the policy uses `tango.NonceSource()`.
- The admin sends no policy by default. Compose `admin.ContentSecurityPolicy()` through `admin.WithMiddleware(tango.SecurityHeaders(tango.WithContentSecurityPolicy(...)))`. Host widgets that emit an inline `style` attribute or an inline event handler are blocked by that policy.
- The Swagger UI page needs `swaggerui.WithContentSecurityPolicy(policy)`, which grants its three relaxations on that route only. `tango.UnsafeInlineStyle()` is valid in `style-src` only.
- `storage.Serve` sends a sandbox policy by default. `storage.ServeOptions{SkipSandboxPolicy: true}` is the typed opt-out.

## Check

- Compare header behaviour with `security_headers.go`, and the admin composition with `admin/content_security_policy.go`.
- Run the shared checklist: `docs/agents/checklist.md`.
