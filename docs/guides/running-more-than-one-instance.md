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
- **A related limit, even with one instance:** see [behind a reverse proxy](#behind-a-reverse-proxy) below.

### `ratelimit`

`ratelimit.Limiter` is a token bucket held per process, so each instance has its own bucket for each key.

- **What you get:** the burst and the sustained rate are each multiplied by up to N across instances.
- **Workaround:** enforce the quota that matters at the edge, and keep `ratelimit` as a second, per-instance guard. Size its `Limit` and `Refill` for one instance's share.
- **Keys:** `ratelimit.RemoteIPKey` with your proxy networks is the supported way to key on the client address behind a proxy, with the same rules as above. See [rate limiting](rate-limiting.md).

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

### A cache

`cache/local` is per process. Each instance has its own, so a value cached on one instance is a miss on the next and a `Delete` reaches only the instance that ran it. A Redis or Memcached cache (`cache/redis`, `cache/memcache`) is shared by every instance and is the right choice for recomputable reads across several. It shares only the cache: a remote cache does not make the login limiter, `ratelimit`, `realtime` rooms or the `accounts` mail outbox shared, and it never holds anything that must be correct or last. See [caching](cache.md).

### Uploaded files

`storage/local` writes to one machine's disk. Another instance cannot read those files, and a container replacement deletes them unless the directory is a persistent volume mounted on exactly one instance. Use `storage/s3` for any deployment with more than one instance or an ephemeral disk. The metadata rows are in your database and are shared; the bytes are what must be reachable from every instance. See [file uploads](uploads.md#choosing-a-backend).

## Behind a reverse proxy

This applies with one instance too. The failed-login limiter needs to know which client a request came from.

- **Direct deployment (no proxy):** no configuration. The limiter counts failures per connection address, and reads no forwarded header.
- **Behind a reverse proxy or load balancer:** the connection address is the proxy's, so every user shares one bucket, and five failed logins from anyone lock everyone out for a minute. Name your proxy's network:

```go
_, proxies, _ := net.ParseCIDR("10.0.0.0/8") // the network your proxy connects from
admin.New(store, admin.WithTrustedProxies(proxies))
accounts.New(store, accounts.WithTrustedProxies(proxies))
```

  With that, and only for a request whose connection comes from inside one of the networks you named, the limiter reads `X-Forwarded-For` from the right, skips addresses that are themselves in your list, and takes the first other address as the client: the address your nearest proxy actually saw. Anything the client wrote further left is never used, so this works whether your proxy appends to the header (nginx's default) or replaces it. A request from anywhere else keeps its own connection address and its header is ignored. `X-Real-IP` is not read, and an entry that is not a bare IP address makes the limiter fall back to the connection address.
- **What to put in the list:** the networks your proxies connect from, and nothing wider. Your proxy must add the address it sees to `X-Forwarded-For`; a proxy that passes the header through untouched leaves the client's own claim as the rightmost entry.
- **Scope:** the failed-attempt limiters only. For admin that is login; for `accounts` it is login, registration, password reset and verification resend, four limiters that use this one rule for naming the client. The `ratelimit` package's `RemoteIPKey` follows the same rules when you give it your proxy networks, so the two cannot disagree about who a client is. Nothing else about proxies is handled.

## Where tanGO's guarantees end

tanGO guarantees the single-process behaviour described above and nothing about the routing or edge layer you add. The workarounds name an approach, not a configuration, because the proxy, balancer or CDN you use is outside what tanGO tests. Check its documentation for how to rate limit by path and to route by key, and test it against your own traffic.

## When this will change

tanGO will revisit shared state when one of four things happens: a named deployment runs more than one instance in production; a second concrete limiter store proves the single atomic `Take` operation [ADR 0029](../adr/0029-ratelimit-is-a-concrete-token-bucket.md) describes; an incident shows the in-memory limiter bypassed through multiple instances; or a real application needs cross-instance realtime rooms or presence. If you are in one of those situations, open an issue describing the deployment.
