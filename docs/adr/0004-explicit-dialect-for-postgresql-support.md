# Explicit `Dialect` argument for PostgreSQL support

## Context

`db.Store` first shipped against SQLite only, with SQL generation (placeholders, PK backfill) hardcoded to SQLite's idioms. Adding PostgreSQL support means `Store` must generate different SQL text for at least two things: placeholder syntax (`?` vs `$1, $2, ...`) and primary-key backfill after `INSERT` (SQLite's `LastInsertId()` vs Postgres's lack of it, requiring `RETURNING`).

`Store` needs to know which dialect it's talking to before it can build correct SQL. Two ways to know:

1. **Auto-detect** from the `*sql.DB`/driver at `NewStore` time — no new API surface, but relies on sniffing driver-specific details (e.g. `reflect.TypeOf(sqlDB.Driver())`), which varies across Postgres driver choices (`pgx`'s `stdlib` adapter vs `lib/pq` register different type names) and is inherently fragile to future driver changes.
2. **Explicit `Dialect` argument** passed to `NewStore` — one extra constructor parameter, but the app author states the dialect once, matching how they already chose and wired the underlying `*sql.DB`.

## Decision

`db.NewStore` takes an explicit `Dialect` value (`db.SQLite` or `db.Postgres`). No driver sniffing.

This matches tanGO's existing bias toward explicit construction over runtime discovery — `tango.NewApp`, `admin.New`, and `Registry.SetStore` all require the caller to state what they mean rather than tanGO inferring it.

## Consequences

- `db.NewStore`'s signature changes (additive: a new required argument) — this was a breaking change to the original `db.NewStore(sqlDB *sql.DB) *db.Store` signature, since the dialect can't safely default. Existing SQLite call sites had to pass `db.SQLite` explicitly.
- SQL generation inside `Store` (`Create`, and any future statement-building) branches on `Dialect` internally; this is a widening of `Store`'s internals, not its public CRUD API shape.
- Raw-SQL escape hatches (`Query`/`QueryRow`) remain dialect-specific by the caller's own hand — `Store` does not attempt to translate or validate that the caller's SQL matches the configured dialect. A mismatch is a caller bug, not one `Store` catches.
- Adding a third dialect later (e.g. MySQL) follows the same pattern: a new `Dialect` value plus whatever branches `Create`'s SQL generation needs — no redesign forced by this decision.
- Because there's no `Config`-driven dialect selection, the driver import and the `Dialect` argument are necessarily written at the same call site by the app author — they cannot silently drift apart into disagreeing config values, since there's only one place either is stated.

**Later addition:** `tango.LoadDBDialectFromEnv` reads `TANGO_DB_DIALECT` so a deployed app can pick its database from the environment. It keeps the core of this decision: nothing is sniffed from the driver, and `main` still passes the dialect to `db.NewStore`, `DispatchFlags` and `ServeContext` explicitly. It gives up the single-call-site guarantee in the last point above: an environment that names a different dialect from the driver the binary opens now shows up as a startup error rather than being impossible to write.
