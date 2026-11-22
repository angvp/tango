# Adopting an existing PostgreSQL database

A billing database that already exists, taken over by tanGO without changing it. `legacy.sql` is that database as an older tool created it: `SERIAL` keys, `timestamp without time zone`, `NOT NULL`, column defaults, a `date` column and explicitly named indexes. The models in `ledger/models.go` describe it exactly, with the tags that make that possible:

| In `legacy.sql` | In the model |
|---|---|
| `id serial PRIMARY KEY` | `tango:"pk,int4"` |
| `timestamp without time zone` | `tango:"naive"` |
| `NOT NULL`, `DEFAULT 'standard'` | `tango:"notnull,default=standard"` |
| `CREATE UNIQUE INDEX idx_customers_email` | `tango:"unique=idx_customers_email"` |
| a `date` column | a `model.Date` field |

`tango makemigrations` wrote `migrations/` from those models. The `ledger` migration in it rebuilds `legacy.sql` column for column, and a replay of the migrations compared with the models finds nothing to change, so adopting the database does not mean altering it. See [exact schema](../../docs/guides/models-and-tags.md#exact-schema) and the [migrations guide](../../docs/guides/migrations.md#exact-schema-changes).

## Adopt it

You need a PostgreSQL server. The first command below starts a throwaway one.

```sh
docker run -d --rm --name ledger-pg -e POSTGRES_PASSWORD=pw -p 127.0.0.1:55432:5432 postgres:17
export TANGO_DB_DSN='postgres://postgres:pw@127.0.0.1:55432/postgres?sslmode=disable'

# The database as it was before tanGO.
psql "$TANGO_DB_DSN" -f legacy.sql

# Record the ledger migration as applied, once, without running it. Take its
# exact name from migrations/ (the file with App: "ledger").
psql "$TANGO_DB_DSN" <<'SQL'
CREATE TABLE IF NOT EXISTS tango_migrations (
    app        text                     NOT NULL,
    name       text                     NOT NULL,
    applied_at timestamp with time zone NOT NULL,
    PRIMARY KEY (app, name)
);
INSERT INTO tango_migrations (app, name, applied_at)
VALUES ('ledger', '0002_auto_20261122210957', now())
ON CONFLICT (app, name) DO NOTHING;
SQL

go run . -migrate              # creates only the admin's tables
go run . -tango-admin-create=admin
go run .
```

Open `http://localhost:8000/admin/` and the customers and invoices are there, and `GET /customers/` lists them as JSON. Without the `INSERT`, `-migrate` would try to create `customers` again and fail on the table that is already there; the test shows both.

## Tests

`go test .` replays the migrations against the models and fails if they differ. With `TANGO_TEST_DSN` set to a PostgreSQL DSN it also builds `legacy.sql` and the `ledger` migration in two schemas and compares their columns, types, nullability, defaults and index names, then runs the adoption above and reads the rows back, including the `Date` and the nullable `PaidAt`.

```sh
TANGO_TEST_DSN='postgres://postgres:pw@127.0.0.1:55432/postgres?sslmode=disable' go test .
```

The migrations need tango v0.10.0 or later. A migration that uses an exact-schema attribute is written in a form an older tango cannot read.
