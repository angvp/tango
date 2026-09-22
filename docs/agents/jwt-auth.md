# Agent Recipe: JWT Authentication

Use this for a stateless JSON API or a transport that cannot rely on browser cookies.

Canonical example: `examples/jwt-api`. Human guide: `docs/guides/jwt-auth.md`.

## Build

- Construct one `auth/jwt.Service` at startup from host-provided secrets.
- Give every key a unique `kid` and at least 32 secret bytes.
- Set an explicit issuer, audience, and maximum TTL.
- Attach `service.Middleware(jwt.BearerToken)` before protected routes.
- Wrap protected views with `jwt.Require`.
- Read identity with `jwt.FromContext(ctx.Context())`; treat `Claims.Subject` as opaque.
- Put issuance behind the host application's own credential or authorization check.

## Choose The Extractor

- Prefer `jwt.BearerToken` for HTTP APIs.
- Use `jwt.QueryToken(name)` only when the transport cannot send a header; query tokens may be logged.

## Don't

- Do not use the example secret or development issuance flag in production.
- Do not expect logout, blacklist, refresh, or early revocation; JWT verification is stateless.
- Do not add application fields to `jwt.Claims`; fetch them using `Subject`.
- Do not expose expired-vs-invalid details in the HTTP response.

## Check

- Exercise missing, malformed, expired, wrong-issuer/audience, and valid tokens.
- Verify retired keys only remain configured until their tokens expire.
- Run `docs/agents/checklist.md` before stopping.
