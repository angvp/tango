# Agent Recipe: Caching

Use this when an app caches an expensive read that is safe to recompute.

Canonical files: `cache/cache.go`, `cache/fetch.go`, `cache/prefix.go`, `cache/local/local.go` and `cachetest/suite.go`. Runnable proof: `examples/cache` (`FetchJSON` over `cache/local`, a versioned prefix, a failing `Store` in its tests). Human guide: `docs/guides/cache.md`.

## Rule

- Depend on `cache.Store` (`Get`, `Set`, `Delete`) and pass it in; construct the concrete store in `main`: `local.New(local.Config{MaxEntries, MaxBytes})` for development and one process, `redis.New(ctx, redis.Config{URL})` or `memcache.New(memcache.Config{Servers})` (their own modules) when several instances must share it.
- Cache only reads you can recompute. Never cache authorization, sessions, rate limits, locks, queues or durable data; the database stays the source of truth.
- A miss is `ok == false` with a nil error; an error means the backend failed. Keys are 1 to 200 printable ASCII bytes without spaces; every TTL is `0 < ttl <= cache.MaxTTL` (30 days). Always set a TTL.
- Read through with `cache.FetchJSON(ctx, store, key, ttl, load, cache.OnError(...))`: it fails open (a cache failure runs the loader and returns its value) and reports failures only to the hook. Put the hook's logging in the app; the cache never logs.
- Namespace with `cache.Prefix(store, "thing:v2:")`; invalidate in bulk by bumping the version. Invalidate one key with `Delete` from the code that changed the data. There is no `Clear`.
- Wrap a hot loader with `golang.org/x/sync/singleflight` yourself; `FetchJSON` does not coalesce.
- Tests: `cache/local` with a `Now` clock; a `Store` whose calls all fail to test fail-open; `cachetest.Run` for a custom `Store`.

Tiny shape:

```go
rate, err := cache.FetchJSON(ctx, store, "rate:EUR", 30*time.Second, load,
	cache.OnError(func(op string, err error) { logger.Warn("cache failure", "op", op, "err", err) }))
```

## Don't

- Do not put a user id, a token or other high-cardinality or secret data in a key you also log or expose.
- Do not rely on `Delete` or a cached value across instances with `cache/local`; it is per process.
- Do not cache a loader error, an authorization decision or a value the request could not have computed itself.
- Do not call `redis.NewFromClient` expecting it to ping or close the client; the host closes it.
- Do not expect response, fragment or ORM caching, or invalidation from `db.Store` writes; they do not exist.

## Check

- Compare wiring with `examples/cache/main.go` and `examples/cache/rates.go`.
- Read `docs/guides/cache.md`, and run `docs/agents/checklist.md`.
