# A cache for a recomputable read

`GET /rates/EUR/` works out an exchange rate once and serves it from a bounded in-process cache for 30 seconds, answering `X-Cache: miss` the first time and `X-Cache: hit` afterwards. It shows the whole of what the `cache` package is for:

- **`cache.FetchJSON` at the call site.** The cache is a `cache.Store` passed in; nothing is global. A hit skips the loader, a miss runs it and stores the result, and a loader error (an unknown currency) is returned and never cached.
- **A bounded store.** `cache/local` holds at most 1000 entries and 1 MiB.
- **A versioned prefix for bulk invalidation.** Entries live under `rates:v1:`. Set `CACHE_VERSION=2` and restart, and every older entry becomes unreachable and expires by its TTL. There is no `Clear`.
- **Fail-open.** If the cache is down, the request still succeeds: `FetchJSON` computes the rate and reports each cache failure through `OnError`, which this app logs. The test uses a tiny `Store` whose every call fails to prove it.

It caches **recomputable reads only**. It is not ORM caching or response caching, and nothing invalidates the cache when the underlying data changes: a stale answer lasts at most the TTL.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/cache
go run .
curl -i localhost:8000/rates/EUR/   # X-Cache: miss
curl -i localhost:8000/rates/EUR/   # X-Cache: hit
```

## Things to know

- `cache/local` is per process. For more than one instance use `cache/redis` or `cache/memcache` (each its own module) and change only `newStore` in `main.go`. See the [caching guide](../../docs/guides/cache.md).
- `go test .` advances the cache's clock by hand, so expiry is tested without sleeping.
