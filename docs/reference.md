# API reference

The supported v0.1 public surface: the root package, `model`, `db`, and `admin` in full; `migration` primarily through its CLI workflow. Anything not listed here that happens to be exported should be treated as an implementation detail that may change without notice. Generated package documentation (`go doc ./...` locally, or [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) once published) complements this page with full doc comments, but this page is the map of what's actually meant for you to use.

## Root package (`github.com/angvp/tango`)

| Symbol | What it's for | See also |
|---|---|---|
| `type App interface{ Name() string; Register(*Registry) error }` | The contract every installable app satisfies — local or reusable. | [project structure](guides/project-structure.md), [reusable apps](guides/reusable-apps.md) |
| `func NewApp(name string, registerFn func(*Registry) error) App` | Build an `App` from a name and a registration closure — the pattern used throughout the tutorial and examples. | [tutorial part 1](tutorial/01-bootstrap-routing-json.md) |
| `type Config struct{ InstalledApps []App; Addr string }` | Minimal project configuration. | [configuration](guides/configuration.md) |
| `func LoadConfigFromEnv() Config` | `Config` with `Addr` from `TANGO_ADDR`. | [configuration](guides/configuration.md) |
| `func LoadDBDSNFromEnv() string`, `func LoadDBDialectFromEnv() (db.Dialect, error)`, `func LoadEnvFile(path string) error` | Optional environment helpers used by generated projects. | [configuration](guides/configuration.md) |
| `func BuildRegistry(config Config) (*Registry, error)` | Registers every `Config.InstalledApps` entry into a new `Registry`. Fails on `ErrDuplicateApp`. | |
| `func Check(config Config) error` | Runs registration, compiles the route tree, and enforces app-contributed checks; the logic behind `tango check`'s `-check` flag. | [app checks](guides/app-checks.md) |
| `func DispatchFlags(config Config, sqlDB *sql.DB, dialect db.Dialect, migrations []migration.Migration) (bool, error)` | Handles the stable app-side flags (`-check`, `-tango-dump-models`, `-tango-status`, `-migrate`, `-migrate -down`) and reports whether serving should stop. | [project structure](guides/project-structure.md) |
| `func Serve(config Config, sqlDB *sql.DB, dialect db.Dialect) error` | Builds the registry, runs registration, wires the `db.Store`, compiles routes, and starts `http.ListenAndServe`. `dialect` must match whatever `sqlDB` was opened with — pass the same value given to `DispatchFlags`. | [project structure](guides/project-structure.md) |
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
| `type FieldMeta struct{ Name string; Type reflect.Type; PrimaryKey, Unique, Indexed, Editable bool; ForeignKey string }` | One field's metadata, derived from its type and `tango` tag. `ForeignKey` is the target model name from `tango:"fk=X"`, or `""`. |
| `type Registry`, `func (*Registry) Register(value any) error`, `func (*Registry) Get(name string) (ModelMeta, bool)`, `func (*Registry) All() []ModelMeta`, `func (*Registry) ValidateForeignKeys() error` | The model registry, reached via `tango.Registry.Models()`. `ValidateForeignKeys` checks every `fk=` target is registered; `tango.Check` calls it automatically. |
| `ErrDuplicateModel`, `ErrNoPrimaryKey`, `ErrMultiplePrimaryKeys`, `ErrEmbeddedField`, `ErrUnsupportedField`, `ErrUnknownForeignKeyTarget` | Sentinel errors from `Register`/`ValidateForeignKeys`. |

See [models and tags](guides/models-and-tags.md) for the full tag/field-kind reference, and [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md) for `fk=`.

## `db` (`github.com/angvp/tango/db`)

| Symbol | What it's for |
|---|---|
| `type Dialect`, `SQLite`, `Postgres` | Which SQL dialect a `Store` generates for. |
| `func NewStore(sqlDB *sql.DB, dialect Dialect) *Store` | Construct a `Store`. |
| `func (*Store) Create/Update(ctx, meta, dest) error`, `func (*Store) Get(ctx, meta, ...) error`, `func (*Store) List(ctx, meta, Query, dest) error` | Metadata-driven CRUD. When `UseModels` has been called, `Create`/`Update` also validate every set foreign key field against an existing related row (referential-integrity validation), failing with `ErrInvalidForeignKey`. See [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md). |
| `func (*Store) Delete(ctx, meta, pk) error`, `func (*Store) UseModels(models *model.Registry)` | Deletes the matching row; when `UseModels` has been called (`tango.Registry.SetStore` does this automatically), also cascades — deleting every row of every other registered model that references it via a foreign key field first, recursively, in one transaction. See [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md). |
| `type Query struct{ Limit, Offset int; OrderBy []string }` | Pagination/ordering for `List`. |
| `func (*Store) Query/QueryRow(ctx, dest, sql string, args ...any) error` | Raw-SQL escape hatches, reusing CRUD's scan-into-`dest` machinery — where `Store` stops and hand-written SQL begins for joins, aggregates, or cross-model filtering. |
| `func ColumnName(name string) string` | The snake_case table/column name derivation, exposed for anything that needs to match it (e.g. building raw SQL by hand). |
| `func SQLiteForeignKeysDSN(dsn string) string` | Appends the SQLite driver's `_foreign_keys=on` DSN parameter, so foreign key constraints are actually enforced on connections opened with the result. |
| `ErrNotFound` | Sentinel for a missing row on `Get`/`Update`/`Delete`. |
| `ErrInvalidForeignKey` | Sentinel for referential-integrity validation failing on `Create`/`Update` — a set foreign key field references a row that doesn't exist. Distinct from `model.ErrUnknownForeignKeyTarget`, which validates the related *model*, not a specific row. |

See [persistence CRUD and raw SQL](guides/persistence-crud-and-raw-sql.md) and [SQLite/PostgreSQL setup](guides/sqlite-and-postgresql-setup.md).

## `admin` (`github.com/angvp/tango/admin`)

| Symbol | What it's for |
|---|---|
| `func New(store *db.Store, opts ...Option) tango.App` | Constructs the admin app. Add it to `InstalledApps` after every app registering models with it. Mounts `/admin/`, `/admin`, `/admin/login/`, and `/admin/logout/` alongside each model's routes. Also registers `AdminUser`/`AdminSession` as ordinary models. `opts` currently only takes `WithBranding`; `New(store)` with none is unchanged. |
| `type AdminUser struct{ ID, Username, PasswordHash, Active, CreatedAt }`, `type AdminSession struct{ ID, Token, UserID, ExpiresAt }` | The Admin account and session models `New` registers. Never registered with the admin's own CRUD registry — manage accounts via the `tango admin` CLI, not the admin UI. |
| `func CreateAccount/ResetPassword/Deactivate(ctx, store, username, ...) error` | The account-lifecycle functions behind `tango admin create/resetpassword/deactivate` — the only code paths that ever write a password, always bcrypt-hashed. |
| `func HandleCLI(ctx, store, args, stdin, stdout, stderr) (bool, error)` | Handles the `-tango-admin-create/-resetpassword/-deactivate` app-side flags a generated `main.go` calls alongside `tango.DispatchFlags`. |
| `type Options struct{ ListDisplay, Search, Ordering []string; Label string; Labels, HelpText map[string]string; ReadOnly, FieldOrder []string; Widgets map[string]Widget }` | Per-model admin configuration, validated against the model's actual fields at registration time. `Label` names the field shown wherever this model is displayed as a related object. `Labels`/`HelpText`/`ReadOnly`/`FieldOrder`/`Widgets` customize the create/edit form — see [admin registration](guides/admin-registration.md#customizing-form-fields). Best-effort, not a stable v0.1 contract — see [limitations](../limitations.md#best-effort-admin-extensibility). |
| `type Widget interface{ Render(FieldContext) template.HTML; Parse(FieldContext, FieldValues, reflect.Value) error }`, `type FieldContext struct{...}`, `type FieldValues interface{ Get, Has(string) ... }` | The field-widget contract set via `Options.Widgets`. `FieldContext` (field name, label, help text, value, read-only flag, related-model select options) is passed to both methods, so a widget derives its own submitted form key(s) from `FieldContext.Name` identically on both sides. See [admin registration](guides/admin-registration.md#custom-field-widgets). Best-effort, like `Options` above. |
| `func Textarea() Widget` | tanGO's one built-in widget beyond the default five (text/number/date-time/checkbox/foreign-key-select) — a multi-line text input for a string field. |
| `type Branding struct{ Name, LogoURL string }`, `func WithBranding(Branding) Option` | The sidebar's only two customization slots — a brand name and logo — passed to `New`. Best-effort, like `Widget` above. See [admin registration](guides/admin-registration.md#branding). |
| `type Registry`, `func NewRegistry(models *model.Registry) *Registry`, `func (*Registry) Register(value any, opts Options) error` | The admin sub-registry, reached via `tango.Registry.Admin()`. |

The default admin theme (sidebar navigation across every registered model, humanized field labels, a Django-admin-style truncated paginator with a true total count) is applied automatically — there's nothing to opt into or configure beyond `Options` and `WithBranding`. Authentication is a real session-cookie login (not HTTP Basic Auth): every form carries a CSRF token, and failed login attempts are rate-limited — see [admin registration](guides/admin-registration.md) for the full model.

## `migration` (`github.com/angvp/tango/migration`)

The supported way to use this package is the CLI workflow — `tango makemigrations`, `tango migrate`, `tango migrate down` — documented in the [migrations guide](guides/migrations.md). Most exported symbols exist so generated migration files, an app's generated `main.go`, and the `tango` CLI can share types across packages.

For reference, what a generated migration file's Go literal actually looks like:

```go
type Migration struct {
	App        string
	Name       string
	Up, Down   []Step
	Reversible bool
}
```

`Step` is implemented by `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, and `DropIndex` — each a plain struct describing one dialect-agnostic schema operation. Treat these as "what generated code looks like," not a hand-authored API.

Stable runtime contract:

| Symbol | What it's for |
|---|---|
| `func ApplyPending(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate`. |
| `func RollbackLast(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate -down`. Returns `ErrNoAppliedMigrations` or `ErrIrreversibleMigration` for their respective cases. |
| `func AppliedMigrations(ctx, sqlDB) (map[MigrationKey]bool, error)` | Reads the applied migration keys from an existing `tango_migrations` table. |
| `type MigrationKey struct{ App, Name string }` | The app-scoped identity used by `tango_migrations`. |
| `func IsMissingTrackingTable(error) bool` | Lets status/checking code treat a missing `tango_migrations` table as "no migrations applied" without creating the table. |
| `type Migration` | The generated migration value passed to `ApplyPending` and `RollbackLast`. |

CLI-internal exported surface:

| Symbol | What it's for |
|---|---|
| `type Step`, `type Column`, `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, `DropIndex` | Generated migration-file representation; don't hand-author these in application code. |
| `type Model`, `func ModelsFromMeta(...)` | The `-tango-dump-models` JSON bridge used by `tango makemigrations`. |
| `func Replay(...)`, `type SchemaState`, `type TableState`, `type ColumnState` | Schema reconstruction for `tango makemigrations`. |
| `func Diff(...)`, `func DiffModels(...)` | Diff engine behind `tango makemigrations`. |
| `func ApplyStep(...)` | Shared DDL translator used by migration runners and tests; use `tango migrate` / `ApplyPending` instead. |
