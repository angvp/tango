# Board

The bulletin board built in the [tutorial](../../docs/tutorial/01-bootstrap-routing-json.md), at its final (part 10) state: JSON API with bearer tokens and rate limits, server-rendered pages with accounts and CSRF-safe forms, the HTML admin, a live front page over WebSockets, a session-pruning job, structured logs, and graceful shutdown.

The code follows the tutorial text; when one changes, change the other.

## Run locally

```sh
echo "BOARD_JWT_SECRET=$(openssl rand -hex 32)" >> .env
go run . -migrate
go run . -tango-admin-create=admin
go run .
go run ./shell -c 'Models()'
go test ./...
```

`TANGO_DB_DSN` picks the database (`sqlite://app.db` by default, or `postgres://…`) and `TANGO_ADDR` the listen address (`:8000`). See part 10 for the full configuration table.

The `Dockerfile` is the one from part 10. In this repository the module resolves `github.com/angvp/tango` through a local `replace => ../..`, so build the image from a copy of the project that depends on a published tanGO version.
