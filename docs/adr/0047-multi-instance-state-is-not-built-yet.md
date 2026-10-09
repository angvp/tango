# Shared state for more than one instance is not built yet

The login limiter, `ratelimit`, `realtime` rooms, and the `accounts` mail outbox and cooldown all live in one process's memory (see [limitations](../limitations.md)). Admin and `accounts` sessions are database rows, so they already work across instances; throttling, rooms and the mail queue do not.

**Decision.** For 0.3.0 tanGO builds no shared state for any of them. It documents what changes with more than one instance and the workarounds, in [running more than one instance](../guides/running-more-than-one-instance.md). No API is added, so nothing new is promised.

**Why.** No tanGO application, including the project's own website, runs more than one instance or plans to. A database-backed limiter, the one piece that needs no new dependency, would still add a table, a migration for every project, a write on every failed login and a behaviour change for existing projects, all without a consumer to shape it. [ADR 0029](0029-ratelimit-is-a-concrete-token-bucket.md) already refuses a storage interface until a second implementation proves its shape (a correct distributed token bucket needs one atomic `Take`, not a generic get/set store), and [ADR 0033](0033-jobs-use-skip-on-overlap-concurrency-with-no-built-in-retry.md) rules out a job queue, which a durable mail outbox would amount to.

**What reopens it.** Any one of:

1. A named tanGO deployment that runs more than one instance in production.
2. A second concrete implementation of the limiter store that proves the `Take` seam ADR 0029 describes.
3. A reported or measured incident where the in-memory limiter was bypassed by spreading requests across instances.
4. A real application that needs cross-instance realtime rooms or presence.

A suspicion that someone might want it is not a trigger.

**Consequences.** `docs/limitations.md` keeps stating the single-process bound and links the guide. The guide stays honest about where tanGO's guarantees end, because the workarounds (routing, proxy rate limiting) live outside tanGO.
