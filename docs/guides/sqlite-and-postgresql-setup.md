# Guide: SQLite, PostgreSQL and MySQL setup

A tanGO project states which dialect it's using through the `db.Dialect` argument passed to `db.NewStore` (and to `DispatchFlags`/`Serve`). There is no driver auto-detection. The value comes from one of two places:

- **The environment** (what `tango newproject` scaffolds): `TANGO_DB_DSN`'s scheme picks the dialect. `tango.LoadDBConfigFromEnv` parses it with `db.ParseDSN`, so the dialect, the driver name, and the connection string all come from one string and can't disagree:

  ```go
  dsn, err := tango.LoadDBConfigFromEnv() // default sqlite://app.db
  sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
  store := db.NewStore(sqlDB, dsn.Dialect)
  ```

  `sqlite://app.db`, `sqlite:///var/data/app.db`, `sqlite://:memory:`, and `postgres://…` and `mysql://…` are the accepted forms; see [configuration](configuration.md#database-env-helpers). The project still imports the driver itself.
- **By hand**, as in the snippets below: the driver import, the connection string, and the `Dialect` value written together at the same call site.

## SQLite (the default)

```go
import _ "modernc.org/sqlite"

sqlDB, err := sql.Open("sqlite", "app.db")
store := db.NewStore(sqlDB, db.SQLite)
```

SQLite is `tango newproject`'s default — zero external services required. `modernc.org/sqlite` is a pure-Go driver (no cgo).

A connection string written by hand like this gets neither the foreign key enforcement nor the 5-second busy timeout that `db.ParseDSN` adds, so a write that finds another connection's lock held fails at once with `database is locked (SQLITE_BUSY)`. Prefer `tango.LoadDBConfigFromEnv` or `db.ParseDSN("sqlite://app.db")`; if you do write it by hand, add both yourself: `app.db?_pragma=busy_timeout(5000)&_foreign_keys=on`.

## PostgreSQL

```go
import _ "github.com/jackc/pgx/v5/stdlib"

sqlDB, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/mydb?sslmode=disable")
store := db.NewStore(sqlDB, db.Postgres)
```

`migration.ApplyStep` and `migration.EnsureTrackingTable` both branch on the `Dialect` you pass through — everything downstream (migrations, `tango_migrations`, `Store`) then generates correct dialect-specific SQL automatically. You never hand-write dialect branches yourself outside this one setup point.

## MySQL

```go
import _ "github.com/go-sql-driver/mysql"

dsn, err := db.ParseDSN("mysql://user:pass@localhost:3306/mydb")
sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
store := db.NewStore(sqlDB, db.MySQL)
```

tanGO supports MySQL 8.4 and later; MariaDB is not tested or supported. Write the `mysql://` form and let `db.ParseDSN` convert it: it sets UTC times, matched-row counts for `UPDATE`, the `utf8mb4_bin` collation, a strict SQL mode and `READ COMMITTED` isolation. Options in the DSN's own query string win, so to run at another isolation level use `mysql://…/mydb?transaction_isolation=%27REPEATABLE-READ%27`. Error messages never echo the DSN.

Create the database yourself first (`CREATE DATABASE mydb CHARACTER SET utf8mb4 COLLATE utf8mb4_bin`); tanGO creates its tables as InnoDB with the `utf8mb4_bin` collation, so `unique` and `=` compare case-sensitively as they do on SQLite and PostgreSQL.

## Practical differences to expect

- **Placeholders**: SQLite and MySQL use `?`; Postgres uses positional `$1, $2, ...`. This only matters for raw SQL you write yourself (`Store.Query`/`QueryRow`) — CRUD methods handle it internally.
- **`ALTER TABLE`**: the migration runner drops a SQLite column by rebuilding the table: create a new one, copy the rows, drop the old one, rename the new one into place, and recreate its indexes. The rebuild keeps everything else about the table (rows, indexes, `UNIQUE`, `REFERENCES`, `DEFAULT`, `NOT NULL`, the primary key's id counter) and other tables' foreign keys into it. It runs in one transaction on one connection, with foreign-key enforcement switched off for that connection only while the old table is dropped, then checks every foreign key before committing; if anything fails, the table is left exactly as it was. Unique constraints and indexes need no rebuild on either dialect: they're `CREATE`/`DROP INDEX`. Postgres steps map straight to `ALTER TABLE`/`CREATE INDEX`. This is invisible in your model/migration code — it only affects what DDL actually runs.
- **Identifier quoting**: every table, column and index name tanGO generates (migration DDL, `Store` statements, admin queries) is quoted — `"user"` on Postgres, `` `user` `` on SQLite and MySQL — so a model or field named after a reserved word (`User`, `Order`, `Group`) works on both. Raw SQL you write yourself (`Store.Query`/`QueryRow`) is passed through untouched, so quote such names there yourself.
- **Timestamps**: `tango_migrations.applied_at` is `TIMESTAMP` on SQLite, `TIMESTAMPTZ` on Postgres and `DATETIME(6)` on MySQL, as is every `timestamp` field; MySQL times are stored and read in UTC.
- **Primary-key backfill**: SQLite backfills via `sql.Result.LastInsertId()`; Postgres uses `INSERT ... RETURNING`; MySQL, which has no `RETURNING`, also uses `LastInsertId()`. All happen transparently inside `Store.Create`.
- **Explicit primary keys**: SQLite's `AUTOINCREMENT` keeps generated IDs past any ID you insert yourself; Postgres's `BIGSERIAL` sequence does not, so `Store.Create` advances it when you supply the primary key. Raw-SQL inserts with explicit IDs on Postgres must advance it themselves — see [explicit primary keys](persistence-crud-and-raw-sql.md#explicit-primary-keys).

- **Text columns on MySQL**: an unbounded `string` field is `LONGTEXT`, which MySQL cannot index. A `string` that is `unique`, `index`ed or the primary key and has no `varchar=n` tag becomes `VARCHAR(255)`, and `Store.Create` and `Store.Update` return a `*db.ValueTooLongError` for a longer value before any SQL runs. Add `varchar=n` to choose another length.
- **`LIKE`**: on MySQL, with the `utf8mb4_bin` collation, `LIKE` is case-sensitive, as on PostgreSQL; SQLite's `LIKE` ignores ASCII case. tanGO does not emulate one behaviour; lower-case both sides in raw SQL if you need a case-insensitive match everywhere.
- **Migrations are not atomic on MySQL**: MySQL commits each DDL statement as it runs, so a migration cannot roll back. Before the first step tanGO checks what it can; if a later step fails, the error names the failed step and every step already applied, no `tango_migrations` row is written and the migration is never resumed on its own. Repair the schema by hand to match, then run the migration again.
- **Foreign keys**: MySQL ignores a column-level `REFERENCES`, so tanGO emits a table-level `CONSTRAINT … FOREIGN KEY` clause.

## Testing against Postgres and MySQL

The test suite runs against one Test dialect per `go test` run, chosen by `TANGO_TEST_DSN` with the same scheme-qualified grammar as `TANGO_DB_DSN`:

```sh
go test ./...                                    # unset: in-memory SQLite
TANGO_TEST_DSN="sqlite://test.db" go test ./...  # SQLite, a fresh file per test (the path is not used)
TANGO_TEST_DSN="postgres://user:pass@localhost:5432/tango_test?sslmode=disable" go test ./...
TANGO_TEST_DSN="mysql://root:pass@localhost:3306/mysql" go test ./...
```

Every package's tests, and every example module's, run this way; CI runs the suite once on each dialect (PostgreSQL 17 and MySQL 8.4). A contributor without a local server still gets a full, green SQLite run. In a Postgres run each test gets its own schema and in a MySQL run its own database (the account needs `CREATE` and `DROP`), dropped when the test ends, and an unreachable database fails the run instead of skipping. A test that cannot run everywhere says so with `testdb.SQLiteOnly`, `PostgresOnly`, `MySQLOnly` or `NotMySQL`. Your own app's tests can use the same helper: `testdb.Open(t)` or `testdb.Store(t)` from `github.com/angvp/tango/testdb`.
