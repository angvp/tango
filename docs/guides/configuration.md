# Guide: configuration

tanGO's configuration surface is deliberately small:

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

- `TANGO_DB_DSN` via `tango.LoadDBConfigFromEnv()`, which parses it with `db.ParseDSN` and returns the dialect, the `database/sql` driver name, and the DSN to pass to `sql.Open`. It defaults to `sqlite://app.db` when unset.

The DSN's scheme picks the database:

| `TANGO_DB_DSN` | Meaning |
|---|---|
| unset | `sqlite://app.db` |
| `sqlite://app.db` | SQLite file relative to the working directory |
| `sqlite:///var/data/app.db` | SQLite, absolute path |
| `sqlite://:memory:` | SQLite, in-memory |
| `postgres://…` / `postgresql://…` | PostgreSQL; passed to pgx unchanged |

Anything else, including a bare `app.db`, fails at startup with an error naming these forms. For SQLite the returned DSN already enables foreign key enforcement (`db.SQLiteForeignKeysDSN`). The app still imports and registers its own driver: `modernc.org/sqlite` for SQLite, `github.com/jackc/pgx/v5/stdlib` for PostgreSQL. There is no separate dialect variable; setting the retired one is a startup error telling you to put the scheme in `TANGO_DB_DSN`.

Admin credentials don't go through an env var at all — accounts live in the database, managed by the `tango admin` CLI (see [admin registration](admin-registration.md)).

`tango.LoadEnvFile(".env")` can load simple `KEY=VALUE` lines before those helpers run. Variables already set in the process environment win over `.env`, so deployment config can override local defaults.
