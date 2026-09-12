# Guide: configuration

tanGO's configuration surface is deliberately small for v0.1:

```go
type Config struct {
	InstalledApps []App
	Addr          string
}
```

- **`InstalledApps`** — the apps that make up your project, in the order they should register. Order is significant: an app's `Register` may depend on state an earlier app already contributed (for example, the admin app in [`examples/api-with-admin`](../../examples/api-with-admin) reads `registry.Admin()` registrations that the `posts` app must have already added). `InstalledApps` is always set by hand in Go code — there is no config file or dynamic discovery.
- **`Addr`** — the address `http.ListenAndServe` binds to when your `main.go` starts the server.

## `LoadConfigFromEnv`

```go
func LoadConfigFromEnv() Config
```

Returns a `Config` with `Addr` read from the `TANGO_ADDR` environment variable, defaulting to `:8000` if unset. This is the only environment-driven configuration in v0.1 — no `.env` file parsing, and `InstalledApps` is never populated from the environment. If you need more configuration (database DSN, feature flags, etc.), read it yourself in `main.go` the same way you would in any Go program; tanGO doesn't get in the way.

```go
config := tango.LoadConfigFromEnv()
config.InstalledApps = []tango.App{posts.New(store)}
```
