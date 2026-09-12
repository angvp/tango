# API reference

The supported v0.1 public surface: the root package, `model`, `db`, and `admin` in full; `migration` primarily through its CLI workflow. Anything not listed here that happens to be exported should be treated as an implementation detail that may change without notice. Generated package documentation (`go doc ./...` locally, or [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) once published) complements this page with full doc comments, but this page is the map of what's actually meant for you to use.

## Root package (`github.com/angvp/tango`)

| Symbol | What it's for | See also |
|---|---|---|
| `type App interface{ Name() string; Register(*Registry) error }` | The contract every installable app satisfies. | [project structure](guides/project-structure.md) |
| `func NewApp(name string, registerFn func(*Registry) error) App` | Build an `App` from a name and a registration closure — the pattern used throughout the tutorial and examples. | [tutorial part 1](tutorial/01-bootstrap-routing-json.md) |
| `type Config struct{ InstalledApps []App; Addr string }` | Minimal project configuration. | [configuration](guides/configuration.md) |
| `func LoadConfigFromEnv() Config` | `Config` with `Addr` from `TANGO_ADDR`. | [configuration](guides/configuration.md) |
| `func BuildRegistry(config Config) (*Registry, error)` | Registers every `Config.InstalledApps` entry into a new `Registry`. Fails on `ErrDuplicateApp`. | |
| `func Check(config Config) error` | Runs registration and compiles the route tree; the logic behind `tango check`'s `-check` flag. | [app checks](guides/app-checks.md) |
| `func DumpModels(config Config) ([]migration.Model, error)` | Registered models in the dialect-agnostic shape `tango makemigrations` diffs against; backs `-tango-dump-models`. | [migrations](guides/migrations.md) |
| `func Status(ctx, config, sqlDB, dialect, migrations) ProjectStatus` | Registration/database/migration status as one payload; backs `-tango-status` and `tango tui`. | |
| `type Registry` | The app/model/route/admin registry apps register into. Key methods: `Register(App) error`, `RunRegistration() error`, `Models() *model.Registry`, `Routes() *RouteRegistry`, `Admin() *admin.Registry`, `SetStore`/`Store`, `Checks() []AppCheck`. | |
| `type AppCheck struct{ Description string; Err error }`, `type Checker interface{ Checks() []AppCheck }` | Optional advisory checks an `App` may contribute. | [app checks](guides/app-checks.md) |
| `func Path(method, pattern string, view View, opts ...RouteOption) Route`, `func Name(name string) RouteOption`, `type URLs []Route` | Declaring routes. | [routing](guides/routing-and-reverse-lookup.md) |
| `func (*RouteRegistry) Include(prefix string, routes []Route) error` | Mounting routes under a namespace prefix. | [routing](guides/routing-and-reverse-lookup.md) |
| `func (*RouteRegistry) Handler() (http.Handler, error)`, `func (*RouteRegistry) Reverser() (Reverser, error)` | Compiling the route tree for serving / reverse lookup. Both require `RunRegistration` to have completed. | [routing](guides/routing-and-reverse-lookup.md) |
| `type Params map[string]string`, `type Reverser interface{ Reverse(name string, params Params) (string, error) }` | Reverse URL lookup. | [routing](guides/routing-and-reverse-lookup.md) |
| `type Context`, `type View func(*Context) error` | The request/response wrapper every view receives: `Param`, `Query`, `Bind`, `JSON`, `Redirect`, `Request`, `ResponseWriter`, `Context`. | [Context and binding](guides/context-and-binding.md) |
| `ErrDuplicateApp`, `ErrMalformedPattern`, `ErrDuplicateRouteName`, `ErrRegistrationNotComplete`, `ErrUnknownRouteName`, `ErrMissingParam` | Sentinel errors, checkable via `errors.Is`. | |

## `model` (`github.com/angvp/tango/model`)

| Symbol | What it's for |
|---|---|
| `type ModelMeta struct{ Name string; App string; Type reflect.Type; Fields []FieldMeta }` | Introspected metadata for one registered model. `Name` is the exact Go type name; `App` records which app registered it. |
| `type FieldMeta struct{ Name string; Type reflect.Type; PrimaryKey, Unique, Indexed, Editable bool }` | One field's metadata, derived from its type and `tango` tag. |
| `type Registry`, `func (*Registry) Register(value any) error`, `func (*Registry) Get(name string) (ModelMeta, bool)`, `func (*Registry) All() []ModelMeta` | The model registry, reached via `tango.Registry.Models()`. |
| `ErrDuplicateModel`, `ErrNoPrimaryKey`, `ErrMultiplePrimaryKeys`, `ErrEmbeddedField`, `ErrUnsupportedField` | Sentinel errors from `Register`. |

See [models and tags](guides/models-and-tags.md) for the full tag/field-kind reference.

## `db` (`github.com/angvp/tango/db`)

| Symbol | What it's for |
|---|---|
| `type Dialect`, `SQLite`, `Postgres` | Which SQL dialect a `Store` generates for. |
| `func NewStore(sqlDB *sql.DB, dialect Dialect) *Store` | Construct a `Store`. |
| `func (*Store) Create/Get/Update/Delete(ctx, meta, ...) error`, `func (*Store) List(ctx, meta, Query, dest) error` | Metadata-driven CRUD. |
| `type Query struct{ Limit, Offset int; OrderBy []string }` | Pagination/ordering for `List`. |
| `func (*Store) Query/QueryRow(ctx, dest, sql string, args ...any) error` | Raw-SQL escape hatches, reusing CRUD's scan-into-`dest` machinery. |
| `func ColumnName(name string) string` | The snake_case table/column name derivation, exposed for anything that needs to match it (e.g. building raw SQL by hand). |
| `ErrNotFound` | Sentinel for a missing row on `Get`/`Update`/`Delete`. |

See [persistence CRUD and raw SQL](guides/persistence-crud-and-raw-sql.md) and [SQLite/PostgreSQL setup](guides/sqlite-and-postgresql-setup.md).

## `admin` (`github.com/angvp/tango/admin`)

| Symbol | What it's for |
|---|---|
| `func New(store *db.Store, credentials Credentials) tango.App` | Constructs the admin app. Add it to `InstalledApps` after every app registering models with it. Mounts `/admin/` and `/admin` (redirecting to the first registered model) alongside each model's routes. |
| `type Credentials struct{ Username, Password string }` | HTTP Basic Auth credentials for admin routes. |
| `type Options struct{ ListDisplay, Search, Ordering []string }` | Per-model admin configuration, validated against the model's actual fields at registration time. |
| `type Registry`, `func NewRegistry(models *model.Registry) *Registry`, `func (*Registry) Register(value any, opts Options) error` | The admin sub-registry, reached via `tango.Registry.Admin()`. |

The default admin theme (sidebar navigation across every registered model, humanized field labels, a Django-admin-style truncated paginator with a true total count) is applied automatically — there's nothing to opt into or configure beyond `Options`.

See [admin registration](guides/admin-registration.md).

## `migration` (`github.com/angvp/tango/migration`)

The supported way to use this package is the CLI workflow — `tango makemigrations`, `tango migrate`, `tango migrate down` — documented in the [migrations guide](guides/migrations.md). You do not typically call this package's functions directly; they exist to be shelled out to by an app's own `main.go` (per the flag-dispatch convention in [project structure](guides/project-structure.md)) and by the `tango` CLI itself.

For reference, what a generated migration file's Go literal actually looks like:

```go
type Migration struct {
	App        string
	Name       string
	Up, Down   []Step
	Reversible bool
}
```

`Step` is implemented by `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, and `DropIndex` — each a plain struct describing one dialect-agnostic schema operation. `Model`/`Column` are the dialect- and reflect-free shapes `-tango-dump-models` emits and `tango makemigrations` diffs against. Treat all of these as "what generated code looks like," not a hand-authored API: the diff/replay internals behind them may still change shape before a stable release.

The functions an app's generated `main.go` calls directly:

| Symbol | What it's for |
|---|---|
| `func ApplyPending(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate`. |
| `func RollbackLast(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate -down`. Returns `ErrNoAppliedMigrations` or `ErrIrreversibleMigration` for their respective cases. |
| `func Replay(migrations []Migration) (SchemaState, error)` | Reconstructs current schema state from migration history; used internally by `tango makemigrations`. |
| `func DiffModels(models []Model, state SchemaState) []Migration`, `func ModelsFromMeta(models []model.ModelMeta) []Model` | The diff engine behind `tango makemigrations`. |
