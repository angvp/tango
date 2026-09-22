# Notes starter

This example is a small tanGO app you could keep as a starting point: one model, JSON routes, generated migrations, the HTML admin, and `ratelimit.Middleware` on the note-creation route.

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

`POST /api/notes/` is rate limited per client IP (5 requests, refilling one every 10 seconds, via `ratelimit.NewLimiter`/`ratelimit.Middleware` in `main.go`) — exceeding it returns `429` with a `Retry-After` header. `GET /api/notes/` is not limited. See [rate limiting](../../docs/guides/rate-limiting.md) for the full model; this example's limits are deliberately small so you can trigger the `429` locally without waiting.

## Configuration

- `TANGO_DB_DSN` defaults to `app.db`.
- `TANGO_DB_DIALECT` defaults to `sqlite`.

To adapt this example to Postgres, switch the driver import, `sql.Open` driver name, `db.Dialect`, and set `TANGO_DB_DSN`/`TANGO_DB_DIALECT`. New projects can generate that shape directly with `tango newproject --dialect=postgres`.
