# Guide: SQLite and PostgreSQL setup

A tanGO project states which dialect it's using through the `db.Dialect` argument passed to `db.NewStore` (and to `DispatchFlags`/`Serve`). There is no driver auto-detection. The value comes from one of two places:

- **The environment** (what `tango newproject` scaffolds): `TANGO_DB_DSN`'s scheme picks the dialect. `tango.LoadDBConfigFromEnv` parses it with `db.ParseDSN`, so the dialect, the driver name, and the connection string all come from one string and can't disagree:

  ```go
  dialect, driverName, dsn, err := tango.LoadDBConfigFromEnv() // default sqlite://app.db
  sqlDB, err := sql.Open(driverName, dsn)
  store := db.NewStore(sqlDB, dialect)
  ```

  `sqlite://app.db`, `sqlite:///var/data/app.db`, `sqlite://:memory:`, and `postgres://…` are the accepted forms; see [configuration](configuration.md#database-env-helpers). The project still imports the driver itself.
- **By hand**, as in the snippets below: the driver import, the connection string, and the `Dialect` value written together at the same call site.

## SQLite (the default)

```go
import _ "modernc.org/sqlite"

sqlDB, err := sql.Open("sqlite", "app.db")
store := db.NewStore(sqlDB, db.SQLite)
```

SQLite is `tango newproject`'s default — zero external services required. `modernc.org/sqlite` is a pure-Go driver (no cgo).

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
- **Identifier quoting**: every table, column and index name tanGO generates (migration DDL, `Store` statements, admin queries) is quoted — `"user"` on Postgres, `` `user` `` on SQLite — so a model or field named after a reserved word (`User`, `Order`, `Group`) works on both. Raw SQL you write yourself (`Store.Query`/`QueryRow`) is passed through untouched, so quote such names there yourself.
- **Timestamps**: `tango_migrations.applied_at` is `TIMESTAMP` on SQLite and `TIMESTAMPTZ` on Postgres, matching each dialect's normal convention.
- **Primary-key backfill**: SQLite backfills via `sql.Result.LastInsertId()`; Postgres uses `INSERT ... RETURNING`, since it has no equivalent. Both happen transparently inside `Store.Create`.
- **Explicit primary keys**: SQLite's `AUTOINCREMENT` keeps generated IDs past any ID you insert yourself; Postgres's `BIGSERIAL` sequence does not, so `Store.Create` advances it when you supply the primary key. Raw-SQL inserts with explicit IDs on Postgres must advance it themselves — see [explicit primary keys](persistence-crud-and-raw-sql.md#explicit-primary-keys).

## Testing against Postgres

The test suite runs against one Test dialect per `go test` run, chosen by `TANGO_TEST_DSN` with the same scheme-qualified grammar as `TANGO_DB_DSN`:

```sh
go test ./...                                    # unset: in-memory SQLite
TANGO_TEST_DSN="sqlite://test.db" go test ./...  # SQLite, a fresh file per test (the path is not used)
TANGO_TEST_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./db/...
```

A contributor without a local Postgres instance still gets a full, green SQLite run. In a Postgres run each test gets its own schema, dropped when the test ends, and an unreachable database fails the run instead of skipping. Your own app's tests can use the same helper: `testdb.Open(t)` or `testdb.Store(t)` from `github.com/angvp/tango/testdb`.
