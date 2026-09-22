# Rate limiting

`github.com/angvp/tango/ratelimit` is a reusable, host-composable request rate limiter, independent of `auth`, `accounts`, `admin`, and `realtime`. It composes as ordinary `tango.Middleware`, so it works on any route — including a WebSocket upgrade route, since an upgrade is just a specially-shaped `GET` request hitting the same routing layer.

The runnable reference is [`examples/notes-starter`](../../examples/notes-starter), which wires `ratelimit.Middleware` onto its note-creation route.

This is deliberately **not** a replacement for `admin`/`accounts`' existing failed-login-attempt limiter (`internal/security.RateLimiter`): that one counts authentication *failures* in a sliding window; `ratelimit` counts *every* request via a token bucket. The two are not merged — sharing internal machinery only makes sense where the semantics are byte-for-byte identical, and they aren't here.

## Constructing a Limiter

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Options{
    Limit:  20,             // bucket capacity — max burst
    Refill: time.Minute/60, // one new token every second
})
```

`Limit` and `Refill` must both be positive — `NewLimiter` rejects zero or negative values at construction, not at first use. A key seen for the first time starts with a **full** bucket, so the first requests for any given key may burst up to `Limit` before being throttled — this is the normal, intended behavior of a token bucket, not a bug to work around.

`Options.Clock` defaults to `time.Now`; it exists mainly so `ratelimit.Middleware`'s own tests (and a host's, if it tests its own middleware stack) can inject a fake clock rather than depending on real-time sleeps. Direct `Limiter.Take` callers always pass an explicit `now` and don't need `Clock` at all.

## The `Decision`

```go
type Decision struct {
    Allowed    bool
    Limit      int
    Remaining  int
    RetryAfter time.Duration
}
```

`Limiter.Take(ctx, key, now, cost)` returns a `Decision` describing whether `key` had `cost` tokens available. `RetryAfter` is derived from the bucket's actual refill state (time until enough tokens accumulate again) — never a hardcoded guess. `cost` defaults to 1 per request via `ratelimit.Middleware` (see below); a caller may charge more for an expensive operation via `WithCost`, or call `Take` directly with any `cost` for non-HTTP use.

## Key extraction

```go
type KeyFunc func(*http.Request) (string, error)

func RemoteIPKey(trustedProxies ...*net.IPNet) KeyFunc
```

`ratelimit` never imports `auth`, `auth/jwt`, or `accounts` — a `KeyFunc` is entirely the host's choice. `RemoteIPKey` is the one default extractor: with no `trustedProxies` given, it only ever uses `r.RemoteAddr`'s host (the same behavior `admin`/`accounts`' own key extraction already has). With trusted CIDRs given, `X-Forwarded-For`/`X-Real-IP` are honored — but **only** when the immediate peer address falls inside one of those CIDRs. From an untrusted peer, those headers are ignored even if present. Never configure `RemoteIPKey` to trust forwarded headers unconditionally: any client could then spoof its own rate-limit key.

A host wanting to key on identity instead of IP (a JWT claim, a session, a tenant ID) writes its own `KeyFunc` using whatever it already has on `*http.Request`/its context.

A `KeyFunc` returning a non-nil `error`, or an empty key with a nil error, is never treated as "this caller is over budget" — both go to a distinct `ErrorHandler` (see below), so an extraction bug can never silently collapse every caller into one shared bucket.

## `Middleware`

```go
func Middleware(limiter *Limiter, key KeyFunc, opts ...MiddlewareOption) tango.Middleware
```

Composes at any of tanGO's existing middleware attachment tiers (global, group, or route — see [routing and middleware](routing-and-reverse-lookup.md#middleware-vs-view-wrappers)). The default rejection response is `429`, `Content-Type: application/json`, a `Retry-After` header (the bucket's real refill time, rounded up to whole seconds — the HTTP header's delta-seconds form), and body `{"error":"rate limit exceeded"}`. The default extraction-failure response is `400`, JSON, `{"error":"rate limit key extraction failed"}`.

Both are overridable:

```go
ratelimit.Middleware(limiter, key,
    ratelimit.WithLimitedHandler(func(w http.ResponseWriter, r *http.Request, d ratelimit.Decision) {
        // build your own response from d
    }),
    ratelimit.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        // build your own response from err
    }),
    ratelimit.WithCost(1), // tokens consumed per request; default 1
)
```

The defaults stay stable and documented — a host overrides them for consistency with its own error envelope, not because the defaults are expected to silently change.

## Deliberate limits

- Single-process, in-memory only — no distributed quota across multiple server processes. See [ADR 0029](../adr/0029-ratelimit-is-a-concrete-token-bucket.md) for why there's no storage interface yet either.
- No tenant-level policy engine or adaptive throttling.
- `admin`/`accounts`' existing failed-login-attempt limiter is untouched and unaffected by this package.

See [limitations](../limitations.md) for the full list.
