# Remote cache adapters are separate Go modules with pinned clients

Redis and Memcached clients bring their own dependency trees, and a project that caches in process should not carry them in its `go.mod`, `go.sum` or vulnerability scan (the reasoning of [ADR 0054](0054-cloud-storage-adapters-are-separate-go-modules.md)). `cache/redis` and `cache/memcache` are therefore each their own Go module, requiring the root module and their client; `cache/local`, `cachetest` and the core stay in the root module with no new dependency. Adapters release on their own tags (`cache/redis/vX.Y.Z`, `cache/memcache/vX.Y.Z`) and may lag the core. Every adapter must pass the same suite, `cachetest.Run`, against a real server in CI (exact service images: `redis:7.4.11-alpine`, `valkey/valkey:8.1.10-alpine` and `memcached:1.6.45-alpine`), and the documentation claims support only for those tested versions, not protocol-wide compatibility.

`cache/redis` uses `go-redis/v9`: every command takes a context, and it supports ACL credentials, TLS and the URL form, and serves Valkey and managed Redis. Its `Config` has no database field (the database is the URL path, which is unambiguous) and no key prefix (`cache.Prefix` is the one way to namespace). `New` pings with the caller's context and closes the client it created if that fails; `NewFromClient` is the escape hatch for Cluster, Sentinel, dynamic credentials and Unix sockets, does not ping, and its `Close` does nothing because the host keeps ownership of the client it supplied. `cache/memcache` uses `gomemcache`, the only maintained client, and its configuration is deliberately asymmetric because the client is: no authentication, TLS only through its dialer, no cancellation of a call in flight (the context is checked before the call and `Timeout` bounds it afterwards), key sharding across servers without replication, and one-second expiry granularity rounded up.

`gomemcache` has no semver tag, so the module pins an exact pseudo-version. That is a known risk: it is mitigated by a small, stable protocol and a pinned commit, and it is revisited if the project becomes unmaintained. Memcached also reports an oversized value as an unstructured server error, which the adapter recognises by its text and maps to `cache.ErrTooLarge`; a test against a real server guards that mapping.

Rejected:

- **Subpackages of the root module.** They leak both client trees into every project.
- **One adapter module for both servers.** Choosing Redis would pull in the Memcached client.
- **A symmetric configuration.** It would promise Memcached authentication and cancellation that its client cannot honestly provide.
- **A bundled fake server for CI.** A real server is the only proof that TTLs, size limits and errors behave as documented.

Consequences: each adapter has its own release and tag process; `go test ./...` at the repository root does not run an adapter, so CI runs each module against its service; and an adapter's configuration struct is Covered in its own module under the same policy.
