# JWT authentication

`github.com/angvp/tango/auth/jwt` provides explicit HS256 access-token primitives for JSON APIs and transports that cannot rely on browser cookies. It is an alternative to tanGO's database-backed cookie sessions, not a replacement for them.

Use JWT when stateless verification is important. Use `auth` or `accounts` cookie sessions when you need immediate revocation: a JWT remains valid until its expiry.

The runnable reference is [`examples/jwt-api`](../../examples/jwt-api). It demonstrates service construction, a protected JSON route, request claims, and a development-only token-issuance flag.

## Constructing a service

Create one immutable `jwt.Service` during application startup:

```go
service, err := jwt.NewService(
    jwt.Key{ID: "2026-09", Secret: []byte(os.Getenv("JWT_SECRET"))},
    nil,
    "bookstore",
    "bookstore-api",
    jwt.WithMaxTTL(30*time.Minute),
)
```

Every key needs a unique, non-empty ID and a secret of at least 32 bytes. Issuer and audience are required. The default maximum token lifetime is 24 hours; `WithMaxTTL` can tighten or extend it explicitly. Clock skew defaults to zero and can be relaxed by at most five minutes with `WithClockSkew`.

Load secrets from secure host configuration. Never copy the example key into a real deployment.

## Issuing and verifying

`service.Issue(subject, ttl)` signs a token with the active key and stamps `sub`, `iss`, `aud`, `iat`, and `exp`. The subject is an opaque host-owned identity string; tanGO does not look up an `accounts.Account` or any other model.

`service.Verify(token)` validates HS256, `kid`, signature, issuer, audience, issue time, and expiry. It returns tanGO-owned `jwt.Claims`, never third-party claim or error types. Use `errors.Is` to distinguish `jwt.ErrExpiredToken` from `jwt.ErrInvalidToken` when application code needs that distinction. HTTP authentication deliberately returns the same generic response for both.

Token issuance belongs behind the host application's own credential or authorization check. The `examples/jwt-api` issuance flag exists only for local development.

## Protecting HTTP routes

Attach `service.Middleware(jwt.BearerToken)` at the global, group, or route middleware tier, then wrap protected views with `jwt.Require`.

The middleware verifies a supplied token once and places `jwt.Claims` on the standard request context. A missing token passes through, which permits optional-auth routes. An invalid or expired supplied token stops with `401 {"error":"unauthorized"}`. `jwt.Require` rejects requests that reached a view without verified claims.

Read claims inside a view with:

```go
claims, ok := jwt.FromContext(ctx.Context())
```

`jwt.Require` does not verify tokens itself. Without the middleware before it, the wrapper always returns 401.

## Query-string tokens

`jwt.QueryToken("token")` is available for transports such as a WebSocket upgrade where a client genuinely cannot send an `Authorization` header. Prefer `jwt.BearerToken` otherwise: query strings commonly appear in browser history and server or proxy access logs.

## Manual key rotation

The service has one active signing key and a fixed set of retired verification-only keys:

1. Deploy a new active key.
2. Move the previous active key into `verificationKeys`.
3. Keep the retired key until every token it signed has expired.
4. Remove it in a later deployment.

The active key is automatically part of the verification set; do not duplicate it in `verificationKeys`. Key sets do not change at runtime.

## Deliberate limits

- HS256 only: no RS256, ES256, JWKS, or external identity-provider verification.
- No refresh tokens or automatic refresh rotation.
- No blacklist or revocation store; expiry is the only containment mechanism.
- No custom claims in v0.0.1; look up application data using `Claims.Subject` when needed.
- No automatic or remote key rotation.

See [limitations](../limitations.md) for the security trade-offs.
