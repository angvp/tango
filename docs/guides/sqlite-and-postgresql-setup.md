# Guide: SQLite and PostgreSQL setup

There is exactly one place a tanGO project states which dialect it's using: the `db.Dialect` argument passed to `db.NewStore`. There is no config-driven dialect selection and no driver auto-detection — the driver import, the connection string, and the `Dialect` value are all written together, by hand, at the same call site, so they can't silently drift apart.

## SQLite (the default)

```go
import _ "modernc.org/sqlite"

sqlDB, err := sql.Open("sqlite", "app.db")
store := db.NewStore(sqlDB, db.SQLite)
```

This is what `tango newproject` scaffolds by default — zero external services required. `modernc.org/sqlite` is a pure-Go driver (no cgo).

## PostgreSQL

```go
import _ "github.com/jackc/pgx/v5/stdlib"

sqlDB, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/mydb?sslmode=disable")
store := db.NewStore(sqlDB, db.Postgres)
```

`migration.ApplyStep` and `migration.EnsureTrackingTable` both branch on the `Dialect` you pass through — everything downstream (migrations, `tango_migrations`, `Store`) then generates correct dialect-specific SQL automatically. You never hand-write dialect branches yourself outside this one setup point.

## Practical differences to expect

- **Placeholders**: SQLite uses `?`; Postgres uses positional `$1, $2, ...`. This only matters for raw SQL you write yourself (`Store.Query`/`QueryRow`) — CRUD methods handle it internally.
- **`ALTER TABLE`**: SQLite can't drop a column or add a unique constraint directly, so the migration runner rebuilds the table (create new, copy data, drop old, rename) for those steps. Postgres steps map straight to `ALTER TABLE`/`CREATE INDEX`. This is invisible in your model/migration code — it only affects what DDL actually runs.
- **Timestamps**: `tango_migrations.applied_at` is `TIMESTAMP` on SQLite and `TIMESTAMPTZ` on Postgres, matching each dialect's normal convention.
- **Primary-key backfill**: SQLite backfills via `sql.Result.LastInsertId()`; Postgres uses `INSERT ... RETURNING`, since it has no equivalent. Both happen transparently inside `Store.Create`.

## Testing against Postgres

Postgres-targeted tests in this repo are opt-in via an environment variable:

```sh
TANGO_TEST_POSTGRES_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./db/...
```

They skip cleanly when the variable is unset — a contributor without a local Postgres instance still gets a full, green SQLite-only test run.
