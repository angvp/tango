# Notes starter

This example is a small tanGO app you could keep as a starting point: one model, JSON routes, generated migrations, and the HTML admin.

## Run locally

```sh
go run . -check
go run . -migrate
go run . -tango-admin-create=admin
go run .
```

`-tango-admin-create` prompts for a password on stdin and creates the account with a bcrypt-hashed password — there is no static credential to set in `.env`. Open `http://localhost:8000/admin/` and log in with that username/password; you'll land on `/admin/login/` first since admin auth is a real session-cookie login, not HTTP Basic Auth.

Try the JSON API:

```sh
curl -X POST http://localhost:8000/api/notes/ \
  -H 'Content-Type: application/json' \
  -d '{"Title":"Hello","Body":"First note","Private":false}'

curl http://localhost:8000/api/notes/
```

## Configuration

- `TANGO_DB_DSN` defaults to `app.db`.
- `TANGO_DB_DIALECT` defaults to `sqlite`.

To adapt this example to Postgres, switch the driver import, `sql.Open` driver name, `db.Dialect`, and set `TANGO_DB_DSN`/`TANGO_DB_DIALECT`. New projects can generate that shape directly with `tango newproject --dialect=postgres`.
