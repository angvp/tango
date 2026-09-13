# Guide: reusable apps

A **reusable app** is a tanGO `App` distributed as its own importable Go package, installable into any host project's `InstalledApps` via an explicit Go import. There is no plugin system, no filesystem scanning, and no dynamic loading — a reusable app is exactly as visible to the compiler and to a reader of `main.go` as any other import.

This distinguishes a reusable app from a local app a host project scaffolds with `tango newapp`: a local app lives inside the host's own module and is never imported by anyone else.

## Public shape

A reusable app package exposes one exported `App` value, or one exported constructor, at its package root:

```go
// Either shape is valid and idiomatic.

var App = tango.NewApp("widgets", registerWidgets)

func New(cfg Config) tango.App {
	return tango.NewApp("widgets", func(r *tango.Registry) error {
		return registerWidgets(r, cfg)
	})
}
```

Choose `var App` when the app needs no configuration; choose `func New(...)` when the host needs to pass it something (a `*db.Store`, feature flags, credentials). Prefer a small `Config`/`Options` struct over a long parameter list once there's more than one option.

The host project installs it exactly like a local app, with no special-casing:

```go
config := tango.Config{
	InstalledApps: []tango.App{
		widgets.App,
		blog.New(blog.Config{}),
	},
}
```

Inside `Register`, a reusable app uses only the same public APIs any app uses — `registry.Models().Register(...)`, `registry.Routes().Include(...)`, `registry.Admin().Register(...)`, and an optional `Checks() []tango.AppCheck` — never anything host-specific. If a reusable app needed registry privileges a local app doesn't have, that would be a sign the app contract itself is too narrow; as of this milestone it isn't.

See [project structure](project-structure.md) for what a **host project** owns (its `main.go`, its `Config`, its DB dialect/DSN) that a reusable app never defines a copy of — a reusable app's own `Config`/`Options` type holds only configuration scoped to itself.

## Naming and collision discipline

Model names, admin registrations, and the SQL table names derived from them are keyed **globally and unnamespaced** — by the bare Go struct name, not by which app registered it. Two reusable apps that both define a `Post` struct will collide: the second `registry.Models().Register` call returns `model.ErrDuplicateModel`, and (had that not stopped it first) both would derive the same table name via `db.ColumnName`.

This is a deliberate boundary, not an oversight: tanGO's model registry does not namespace by app label the way some frameworks do, because doing so would require threading an app-qualified key through persistence, migrations, and admin everywhere a bare model name is used today — a much bigger change than this milestone's goal of proving the existing app contract is enough. Avoiding collisions is therefore a **naming convention for reusable app authors**, not a framework guarantee:

- Give exported model types names that are unlikely to collide across independently-authored packages — prefer a descriptive, package-specific name (`WidgetOrder`) over a generic one (`Order`) if the package is meant to be installed alongside other reusable apps.
- Routes don't have this problem today: `Include(prefix, routes)` already namespaces every named route under `prefix`'s trimmed form (e.g. `Include("/widgets/", ...)` combined with `tango.Name("list")` yields the fully-qualified route name `"widgets:list"`), so two reusable apps using distinct URL prefixes never collide on route names, even if their view names are similar.
- If a genuine collision boundary matters for your project (e.g. installing two apps you don't control the naming of), the accepted mitigation for v0.1 is choosing different type names before installing both — not a registry-level workaround.

## Contributed migrations

A reusable app may ship its own migrations, distinct from the migrations a host project generates against its own models. The app exports the same shape a host project's own `migrations/migrations.go` already has:

```go
// package widgets

var Migrations = []migration.Migration{
	// ... generated migration values
}
```

An app generates this slice by running `tango makemigrations` against a small, throwaway generation harness kept inside the app's own repository — effectively treating the app's own repo as a single-app "host" purely so `tango makemigrations` has something to diff against. This harness's `Config` (or whatever local config type it uses) is scoped to the app only: it never defines or overrides host-level general configuration such as DB dialect or DSN. The harness needs a `*db.Store` to satisfy `DumpModels`'s dependencies even though `DumpModels` performs no DB I/O; use a real but throwaway in-memory SQLite store (`db.NewStore` over a `:memory:` `sql.DB`) rather than a `nil` store, since `db.NewStore`'s nil-safety isn't a documented guarantee.

The host project's `main.go` then concatenates the app's `Migrations` with its own host-generated migrations, in `InstalledApps` order, before handing the combined slice to `DispatchFlags`/`ApplyPending`/`RollbackLast`:

```go
var allMigrations []migration.Migration
allMigrations = append(allMigrations, widgets.Migrations...)
allMigrations = append(allMigrations, migrations.Migrations...) // the host's own

tango.DispatchFlags(config, sqlDB, dialect, allMigrations)
```

`tango_migrations` already tracks applied migrations by `(App, Name)` via `migration.MigrationKey`, so contributed and host migrations coexist in the same tracking table without ambiguity as long as each migration's `App` field is set to the owning app's name.

## App-owned templates and static assets

A reusable app that needs its own HTML templates or static files embeds and serves them itself — there is no separate framework mechanism for this. Embed the assets with a package-level `embed.FS`, and serve them through the app's own routes, registered the same way any other route is:

```go
//go:embed static/*
var staticFS embed.FS

func (a App) Register(r *tango.Registry) error {
	return r.Routes().Include("/widgets/", tango.URLs{
		tango.Path("GET", "/static/*", serveStatic(staticFS), tango.Name("static")),
	})
}
```

This is the same pattern the admin package itself uses for its own templates and static assets — reusable apps get no special treatment.
