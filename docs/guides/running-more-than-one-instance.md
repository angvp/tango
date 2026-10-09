# Running more than one instance

tanGO is built for one server process per application. This guide is for the case where you run more than one anyway (a replica count above 1, a load balancer in front of two processes, a rolling deploy where the old and new process overlap) and want to know what still works, what quietly weakens, and what to do about it. Why tanGO does not build shared state for this yet, and what would change that, is in [ADR 0047](../adr/0047-multi-instance-state-is-not-built-yet.md).

## What is already safe

Everything that lives in the database works across instances, because every instance reads the same rows:

- **Sessions.** Admin and `accounts` sessions are database rows. A login on one instance is valid on all of them, and a logout, a password reset or a deactivation ends the session everywhere.
- **Accounts and tokens.** A password-reset or verification link emailed by one instance can be used on any other. Its token is a single-use database row.
- **Your own data and migrations.** Run `-migrate` once per deploy, not once per instance.

## What changes, piece by piece

Each of these keeps its state in one process's memory. With N instances behind a balancer that spreads requests evenly, each instance sees roughly 1/N of the traffic and counts only that.

### The login limiter (admin and `accounts`)

The failed-login limiter allows 5 failures per minute per source address on each of admin login, `accounts` login and `accounts` registration. Each instance keeps its own count.

- **What you get:** up to about N times the intended attempts per minute, and the count resets whenever an instance restarts.
- **What it still does:** every instance stops a single address that hammers it.
- **Workaround:** put a rate limit in front of the instances, on the login and registration paths, at your reverse proxy, load balancer or CDN. That limit is shared by construction.
- **A related limit, even with one instance:** the limiter keys on the connection's remote address and never reads `X-Forwarded-For`. Behind a proxy that is the proxy's address, so all clients share one key unless the proxy preserves the client address at the connection level. Check this before relying on the limiter behind a proxy.

### `ratelimit`

`ratelimit.Limiter` is a token bucket held per process, so each instance has its own bucket for each key.

- **What you get:** the burst and the sustained rate are each multiplied by up to N across instances.
- **Workaround:** enforce the quota that matters at the edge, and keep `ratelimit` as a second, per-instance guard. Size its `Limit` and `Refill` for one instance's share.
- **Keys:** `ratelimit.RemoteIPKey` with trusted proxies is the supported way to key on the client address behind a proxy. See [rate limiting](rate-limiting.md).

### `realtime` rooms

A `Hub` holds its rooms and peers in the process that accepted the connection. A room exists on the instance where its first participant joined, and another instance knows nothing about it.

- **What breaks:** two participants of the same room who land on different instances are in two different rooms. There is no cross-instance message delivery and no cross-instance presence, and a restart loses room state.
- **Workaround:** make every connection for a room reach the same instance. Route on a stable room key (a path segment or header) at the proxy, using whatever consistent routing it offers, so that a given room id always maps to the same instance. tanGO does not provide or verify this routing.
- **Deploys:** a restart drops that instance's rooms and their state. Participants have to rejoin, and they only meet again if your routing sends them to the same instance.

### The `accounts` mail outbox and cooldown

With `accounts.WithMail`, emails wait in an in-memory outbox in the instance that took the request, and the per-address cooldown (five minutes) is kept there too.

- **What you get:** a person who asks again within the cooldown, and reaches a different instance, can trigger another email, so up to N emails per cooldown window for one address. Anything queued when an instance dies is lost, and a failed send is never retried.
- **What still holds:** the link in any email that is sent works on every instance, because the token is a database row.
- **Workaround:** apply the edge rate limit to `/accounts/password-reset/` and `/accounts/verify/resend/`. Give the instance a long enough shutdown timeout for its outbox to drain on a deploy; see [application lifecycle](application-lifecycle.md).

## Where tanGO's guarantees end

tanGO guarantees the single-process behaviour described above and nothing about the routing or edge layer you add. The workarounds name an approach, not a configuration, because the proxy, balancer or CDN you use is outside what tanGO tests. Check its documentation for how to rate limit by path and to route by key, and test it against your own traffic.

## When this will change

tanGO will revisit shared state when one of four things happens: a named deployment runs more than one instance in production; a second concrete limiter store proves the single atomic `Take` operation [ADR 0029](../adr/0029-ratelimit-is-a-concrete-token-bucket.md) describes; an incident shows the in-memory limiter bypassed through multiple instances; or a real application needs cross-instance realtime rooms or presence. If you are in one of those situations, open an issue describing the deployment.
