# Agent Recipe: Running More Than One Instance

Use this when advising on, or documenting, an app that runs on more than one server process (replicas, a balancer, an overlapping rolling deploy).

Canonical files: `internal/security/ratelimit.go`, `ratelimit/limiter.go`, `realtime/hub.go`, `accounts/outbox.go`, `accounts/mail.go`. Human guide: `docs/guides/running-more-than-one-instance.md`. Decision: `docs/adr/0047-multi-instance-state-is-not-built-yet.md`.

## Rule

- Database rows are shared across instances: admin and `accounts` sessions, accounts, and reset and verification tokens. A session or emailed link works on every instance.
- Memory is per instance: the failed-login limiter (5 per minute per address, on admin login, `accounts` login and registration), `ratelimit.Limiter` buckets, `realtime.Hub` rooms and the `accounts` mail outbox and five-minute cooldown. With N instances the limits weaken by up to N, rooms split, and a queued email is lost if its instance dies.
- Advise the workaround, not new code: rate-limit login, registration and mail paths at the proxy or CDN; route every connection for a realtime room to one instance by a stable room key; run `-migrate` once per deploy.
- The login limiter keys on the connection's remote address and reads no forwarded header unless told which proxies to trust: `admin.WithTrustedProxies` and `accounts.WithTrustedProxies` (opt-in; direct deployments need nothing). Behind a proxy without it, every user shares one bucket.

## Don't

- Do not add a storage interface, a Redis dependency or a database-backed limiter: the ADR defers them until one of its four triggers is met.
- Do not give vendor-specific proxy configuration as tested behaviour; tanGO does not test the edge layer.
- Do not state that realtime rooms work across instances.
