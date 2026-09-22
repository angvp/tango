# Agent Recipe: Rate Limiting

Use this to throttle a route (including a WebSocket upgrade route) without coupling to `auth`/`accounts`/`admin`.

Canonical example: `examples/notes-starter`. Human guide: `docs/guides/rate-limiting.md`.

## Build

- Construct one `*ratelimit.Limiter` via `ratelimit.NewLimiter(ratelimit.Options{Limit, Refill})` — both must be positive.
- Choose or write a `ratelimit.KeyFunc`; `ratelimit.RemoteIPKey()` is the IP-only default.
- Wrap the route(s) with `ratelimit.Middleware(limiter, key)` at whichever attachment tier fits (global, group, or route).
- Trust `X-Forwarded-For`/`X-Real-IP` only via `RemoteIPKey(trustedCIDRs...)`, never unconditionally.

## Choose The Shape

- One `Limiter` per distinct policy (a login route and a public API route almost always want different `Limit`/`Refill` values — construct two `Limiter`s, not one shared one with a workaround).
- Default `WithCost(1)` per request is right for uniform endpoints; use `WithCost(n)` for one expensive route — `n` must not exceed that `Limiter`'s own `Limit`, or every request fails outright (`Take` rejects a cost it could never satisfy).
- Override `WithLimitedHandler`/`WithErrorHandler` only when the default JSON responses don't match your app's error envelope.

## Don't

- Do not reuse `admin`/`accounts`' internal failed-login-attempt limiter for general request throttling — different semantics (failures-only vs. every request).
- Do not treat a `KeyFunc` error the same as a rejected request — they go to `ErrorHandler`/`LimitedHandler` respectively, on purpose.
- Do not expect a distributed/shared-state limiter — `ratelimit` is single-process, in-memory only in v0.0.1.
- Do not trust `X-Forwarded-For`/`X-Real-IP` without configuring `trustedProxies` — an untrusted peer can forge them.

## Check

- Exercise: allowed request, rejected request (default and custom `LimitedHandler`), `KeyFunc` error, empty key.
- Verify a request from an untrusted peer with a forged `X-Forwarded-For` is still keyed on its real address.
- Run `docs/agents/checklist.md` before stopping.
