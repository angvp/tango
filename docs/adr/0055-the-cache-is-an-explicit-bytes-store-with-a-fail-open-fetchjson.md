# The cache is an explicit bytes store with a fail-open `FetchJSON`

A cache that is ambient (configured globally, applied to queries or responses, invalidated by writes) quietly becomes a second source of truth, and its failures become correctness bugs. tanGO's cache is the opposite: a `cache.Store` a host passes to the code that wants caching, which can only make a recomputable read cheaper.

The `Store` is bytes-only with three operations, `Get`, `Set` and `Delete`. A miss is `ok == false` with a nil error, so a hit, a miss and a backend failure are three distinct outcomes; a returned slice belongs to the caller and `Set` keeps none of the caller's memory, so every backend, in-process or remote, behaves the same. Keys follow one rule every backend can store (1 to 200 bytes of printable ASCII, no spaces) and a TTL has one portable range (`0 < ttl <= 30 days`), both checked before any request, with backends rounding a TTL up to their granularity and never turning it into "no expiry". Values are bytes whose format is the host's; typed use is two small generic functions, `GetJSON` and `SetJSON`, with no hidden or configurable codec, and a value that no longer decodes is `ErrCorrupt`, never a zero value passed off as a hit.

The one built-in read-through, `FetchJSON`, is explicitly **fail-open**, because that is what a cache is for: a read or decode failure is reported to an optional `OnError` hook and the loader runs; a write failure after a successful load is reported and the loaded value is still returned; a loader failure is returned as it is and is not a cache error. Without a hook a cache failure alongside a loader failure is returned joined, and a successful fallback returns the value and `nil`, which the caller chose by calling `FetchJSON`. The store, the adapters and `FetchJSON` never log and emit no metrics (the same position as storage); the hook is where the host records failures. Invalidation is `Delete` and a versioned `Prefix`, nothing more: no `Clear`, tags or invalidation from `db.Store` writes. `cache/local` is bounded by a required entry limit and an optional byte budget, evicting expired entries first and then the least recently used, with no background goroutine.

Rejected:

- **`any` values or a store-level codec.** They hide an encoding decision and lose type information on remote backends.
- **A fail-open mode on every adapter.** It scatters the policy across backends; one named helper keeps it visible.
- **Automatic caching of queries, models or responses.** It makes the database no longer authoritative and needs invalidation tanGO cannot do correctly.
- **An absolute-time TTL above 30 days.** It makes the contract clock-dependent and collides with Memcached's expiry representation; long retention belongs in the database or object storage.
- **In-process coalescing in `FetchJSON`.** It protects one instance only and can pass one caller's cancellation to the others; a host wraps its loader with `singleflight`.
- **`Clear`, multi-get, increment and set-if-absent.** No named consumer, and not all faithfully available on every backend.

Consequences: a host writes its own keys, TTLs and invalidation; a stale answer lasts at most its TTL; and the `cache` surface, its errors, the key and TTL rules and the conformance suite join the Covered API, while the stored byte format and key layout stay the host's.
