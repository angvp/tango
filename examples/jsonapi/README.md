# JSON API

The smallest tanGO JSON application: one hand-written app, `greetings`, installed in `main.go`, answering one named route. Read `apps/greetings/app.go` for the app and `main.go` for how it's installed: `tango newapp` creates an app's stub, but adding it to `InstalledApps` is always a line you write yourself.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/jsonapi
go run .
```

Then:

```sh
curl http://localhost:8000/greetings/World/
```

which answers `{"message":"Hello, World!"}`. `go test .` checks the same request.

## Configuration

- The address is `TANGO_ADDR`, else the `PORT` hosting platforms set, else `:8000`.
- `TANGO_DB_DSN` picks the database (default `sqlite://app.db`); this example has no models, so it's only used by `-tango-status` and `-migrate`.
- Ctrl-C or `SIGTERM` shuts the server down gracefully.

See the [routing guide](../../docs/guides/routing-and-reverse-lookup.md).
