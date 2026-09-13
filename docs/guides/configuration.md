# Guide: configuration

tanGO's configuration surface is deliberately small for v0.1:

```go
type Config struct {
	InstalledApps []App
	Addr          string
	Middleware    []Middleware
}
```

- **`InstalledApps`** — the apps that make up your project, in the order they should register. Order is significant: an app's `Register` may depend on state an earlier app already contributed (for example, the admin app in [`examples/api-with-admin`](../../examples/api-with-admin) reads `registry.Admin()` registrations that the `posts` app must have already added). `InstalledApps` is always set by hand in Go code — there is no config file or dynamic discovery.
- **`Addr`** — the address `http.ListenAndServe` binds to when your `main.go` starts the server.
- **`Middleware`** — raw `net/http` middleware applied globally to every compiled route, including routes contributed by reusable apps such as admin. The first entry is outermost. See [routing and reverse lookup](routing-and-reverse-lookup.md#middleware-vs-view-wrappers).

## `LoadConfigFromEnv`

```go
func LoadConfigFromEnv() Config
```

Returns a `Config` with `Addr` read from the `TANGO_ADDR` environment variable, defaulting to `:8000` if unset. `InstalledApps` is never populated from the environment; your project composition stays visible in Go.

```go
config := tango.LoadConfigFromEnv()
config.InstalledApps = []tango.App{posts.New(store)}
```

## Database env helpers

The scaffold also uses small standalone helpers, not a separate config language:

- `TANGO_DB_DSN` via `tango.LoadDBDSNFromEnv()`.
- `TANGO_DB_DIALECT` via `tango.LoadDBDialectFromEnv()`, defaulting to SQLite.

Admin credentials don't go through an env var at all — accounts live in the database, managed by the `tango admin` CLI (see [admin registration](admin-registration.md)).

`tango.LoadEnvFile(".env")` can load simple `KEY=VALUE` lines before those helpers run. Variables already set in the process environment win over `.env`, so deployment config can override local defaults.
