# Reusable greetings: the host

A host project installing a reusable app from another module. It imports [`reusable-greetings`](../reusable-greetings) by its module path (a `replace` in `go.mod` stands in for a published version), installs it in `InstalledApps` beside its own `echo` app, applies the reusable app's contributed migrations before its own, and serves the app's routes and embedded static assets. Nothing of the reusable app is copied here.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/reusable-greetings-host
go run . -migrate
go run .
```

Then:

```sh
curl -X POST http://localhost:8000/greetings/ -d '{"Name":"Ada"}'
curl http://localhost:8000/greetings/
curl http://localhost:8000/greetings/static/hello.txt
curl http://localhost:8000/echo/hi/
```

`go test .` checks the installed app's routes, migrations and asset.

## Configuration

- `TANGO_DB_DSN` picks the database and defaults to `sqlite://app.db`.
- The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`.
- Ctrl-C or `SIGTERM` shuts the server down gracefully.

See the [reusable apps guide](../../docs/guides/reusable-apps.md).
