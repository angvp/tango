# Guide: caching

`github.com/angvp/tango/cache` is a small, explicit cache for reads that are expensive and safe to compute again. A `cache.Store` is passed to the code that uses it and called where caching is wanted; nothing about it is global, ambient or automatic. The runnable proof is [`examples/cache`](../../examples/cache). [ADR 0055](../adr/0055-the-cache-is-an-explicit-bytes-store-with-a-fail-open-fetchjson.md) records why it has this shape.

## When to use a cache, and when not

Use it to make a **recomputable read** cheaper: a rate worked out from several sources, a rendered fragment you build yourself, a lookup behind a slow API. Every value you store must be something you could compute again, because a cache can be empty, expire a value early on a small backend, or be down.

Do not use it for anything that must be right or must last:

- **Durable data.** Keep it in your database or object storage. The portable retention is at most 30 days, and a cache may forget sooner.
- **Authorization, sessions, rate limits, locks or queues.** A miss or a failure must never change who may do what.
- **Anything the database must stay authoritative about.** The cache accelerates reads; `db.Store` is the source of truth. tanGO does not cache database queries or models, and does not invalidate anything when you write.

## The Store

```go
type Store interface {
	Get(ctx context.Context, key string) (value []byte, ok bool, err error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
```

A **miss is not an error**: `Get` returns `ok == false` with a nil error, including for an expired value. An error always means the backend failed, so a hit, a miss and a failure are three different outcomes. `Get` returns a slice you own and `Set` does not keep yours, on every backend.

**Keys** are 1 to 200 bytes of printable ASCII with no spaces (`cache.ValidKey`), a rule every backend can store. Anything else is `cache.ErrInvalidKey` before any request. **TTLs** are `0 < ttl <= 30 days` (`cache.MaxTTL`); anything else is `cache.ErrInvalidTTL`. There is no "forever": a TTL is how long a stale answer may be served. A backend with coarser time resolution rounds up, never down, so a value never expires sooner than you asked. A value a backend refuses for size is `cache.ErrTooLarge`; treat values over about 1 MiB as not portable.

The store and the helpers never log. Errors come back to you, and you decide what is worth recording.

## Namespaces and invalidation

```go
profiles := cache.Prefix(store, "profile:v2:")
```

`Prefix` returns a `Store` that puts the prefix in front of every key and validates the final key, so a prefix that makes a key too long is `ErrInvalidKey`. There is deliberately no `Clear`, no tags and no automatic invalidation. The two tools are:

- **`Delete`** a key you know is stale, from the code that changed the data.
- **A new prefix version** to invalidate in bulk: change `profile:v2:` to `profile:v3:` and every older entry becomes unreachable and expires by its TTL.

Invalidation is your responsibility. If a stale answer for the length of the TTL is not acceptable, shorten the TTL or do not cache that read.

## Typed values and read-through

The core stores bytes, and their format is yours. For JSON there are three small generic functions, with no hidden or configurable codec:

```go
rate, ok, err := cache.GetJSON[Rate](ctx, store, "rate:EUR")
err = cache.SetJSON(ctx, store, "rate:EUR", rate, time.Minute)
```

A stored value that no longer decodes is `cache.ErrCorrupt` (a `*cache.CorruptError` with the key and the decode error), never a zero value passed off as a hit. For another format such as protobuf or gob, write the same few lines over the byte `Store`.

`cache.FetchJSON` is the one built-in read-through:

```go
rate, err := cache.FetchJSON(ctx, store, "rate:EUR", 30*time.Second,
	func(ctx context.Context) (Rate, error) { return compute(ctx, "EUR") },
	cache.OnError(func(op string, err error) { logger.Warn("cache failure", "op", op, "err", err) }),
)
```

It **fails open**, on purpose. If the cache read or decode fails, it reports the failure and calls your loader; if the cache write fails after a successful load, it reports it and still returns the value. A cache outage slows the request down and never fails it. `OnError` is where you log or count those failures; it is called synchronously, once per failed cache operation, and cannot change what `FetchJSON` does. Your loader's own error is returned as it is and is never sent to the hook. Without a hook, a successful fallback returns the value and `nil`, and if both the cache and the loader fail, the errors are returned joined.

### Stampedes

When a hot key expires, every concurrent request calls the loader. `FetchJSON` does not coalesce them: in-process coalescing would protect only one instance and can pass one caller's cancellation to the others. If your loader is costly under load, wrap it yourself with `golang.org/x/sync/singleflight`, or add a little jitter to your TTLs.

## Backends

| | `cache/local` | `cache/redis` | `cache/memcache` |
|---|---|---|---|
| Shared between instances | No | Yes | Yes |
| Survives a restart | No | Depends on the server | No |
| Dependencies | None | `go-redis`, in its own module | `gomemcache`, in its own module |
| Use it for | Development, tests, one process | More than one instance | More than one instance |

`cache/local` is a bounded in-process cache. It never grows past `MaxEntries` (required) and the optional `MaxBytes` (the full key plus the value), dropping expired entries first and then the least recently used. It starts no goroutine and needs no shutdown. A single entry bigger than `MaxBytes` is refused with `ErrTooLarge`.

```go
store, err := local.New(local.Config{MaxEntries: 10_000, MaxBytes: 64 << 20})
```

`cache/redis` and `cache/memcache` are separate Go modules, so a project that never imports them never sees a Redis or Memcached client in its `go.mod`. Both pass the same conformance suite, `cachetest.Run`, as `cache/local`; run it against your own `Store` too.

**Redis and Valkey** (`github.com/angvp/tango/cache/redis`): `redis.New(ctx, redis.Config{URL: "redis://host:6379/3"})` where the database is the URL path, `rediss://` for TLS, optional `Username`, `Password`, timeouts and pool size. `New` pings the server and fails fast on a bad address or credentials. For Cluster, Sentinel, rotating credentials or a Unix socket, build the client yourself and wrap it with `redis.NewFromClient`; that function does not ping and its `Close` does nothing, so you keep closing the client you supplied. Support is claimed only for the Redis and Valkey versions in CI.

**Memcached** (`github.com/angvp/tango/cache/memcache`): `memcache.New(memcache.Config{Servers: []string{"cache1:11211"}})`. Memcached differs from Redis and the adapter does not hide it:

- **No authentication.** The client has no SASL. Keep Memcached on an isolated network, or use TLS (`Config.TLSConfig`, for a server started with `--enable-ssl`).
- **No in-flight cancellation.** A cancelled context stops a call from starting; once it is running, `Config.Timeout` (default 500 ms) is the bound.
- **Several servers shard the keys** with no replication. Changing the list remaps keys, which shows up as misses.
- Expiry has one-second granularity, rounded up.

## Across more than one instance

`cache/local` is per process: each instance has its own, so a value cached on one is a miss on the next, and a `Delete` reaches only one. A Redis or Memcached cache is shared, which suits recomputable reads. It does not make anything else shared: the login limiter, `ratelimit`, `realtime` rooms and the `accounts` mail outbox stay in each process. See [running more than one instance](running-more-than-one-instance.md#a-cache).

## What this is not

- No response, fragment, template or ORM caching, and no cache-backed sessions, rate limiting or locks.
- No automatic invalidation from `db.Store` writes.
- No metrics or logging from the cache itself; wrap a `Store` if you want metrics.
- No `Clear`, multi-get, increment or set-if-absent.

## Testing

`cache/local` with its `Now` clock is the test double: advance the clock to expire entries without sleeping. To prove your fail-open behaviour, give `FetchJSON` a tiny `Store` whose every call returns an error, as `examples/cache` does.
