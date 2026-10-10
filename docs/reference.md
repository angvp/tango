# API reference

The supported public surface: the root package, `model`, `db`, `auth`, `accounts`, `i18n`, and `admin` in full; `migration` primarily through its CLI workflow, and `shell` for `tango shell`. Anything not listed here that happens to be exported should be treated as an implementation detail that may change without notice. Generated package documentation (`go doc ./...` locally, or [pkg.go.dev](https://pkg.go.dev/github.com/angvp/tango) once published) complements this page with full doc comments, but this page is the map of what's actually meant for you to use.

For coding-agent-oriented recipes, see the root `AGENTS.md` and `docs/agents/`. Those files are compact task checklists and pointers to canonical examples, not a separate API surface.

## Root package (`github.com/angvp/tango`)

| Symbol | What it's for | See also |
|---|---|---|
| `type App interface{ Name() string; Register(*Registry) error }` | The contract every installable app satisfies — local or reusable. | [project structure](guides/project-structure.md), [reusable apps](guides/reusable-apps.md) |
| `func NewApp(name string, registerFn func(*Registry) error) App` | Build an `App` from a name and a registration closure — the pattern used throughout the tutorial and examples. | [tutorial part 1](tutorial/01-bootstrap-routing-json.md) |
| `type Config struct{ InstalledApps []App; Addr string; Middleware []Middleware; MiddlewareScope MiddlewareScope; NotFound, MethodNotAllowed View }` | Minimal project configuration. `Middleware` wraps every compiled route globally, first entry outermost; `MiddlewareScope` decides whether it also wraps Unmatched requests; `NotFound`/`MethodNotAllowed` optionally replace the router's 404/405 (with `Allow` set for 405). | [configuration](guides/configuration.md), [routing](guides/routing-and-reverse-lookup.md#middleware-vs-view-wrappers) |
| `type MiddlewareScope int`; `MiddlewareScopeDefault`, `MiddlewareScopeRoutes`, `MiddlewareScopeAll` | What `Config.Middleware` wraps. `Routes` wraps each matched route. `All` wraps the whole router, resolving each request's route first and reporting an Unmatched request as `"(unmatched)"`. The zero value is the framework's default (`Routes`), and an unknown value fails `BuildRegistry`. | [middleware scope](guides/routing-and-reverse-lookup.md#middleware-scope) |
| `func LoadConfigFromEnv(...ConfigOption) Config`, `func WithPortFromEnv() ConfigOption` | `Config` with `Addr` from `TANGO_ADDR`, else `:8000`. With `WithPortFromEnv`, `PORT` (as `":"+PORT`) is the fallback before `:8000`. | [configuration](guides/configuration.md#loadconfigfromenv) |
| `func LoadDBConfigFromEnv() (db.DSN, error)`, `func LoadEnvFile(path string) error` | Optional environment helpers used by generated projects. `LoadDBConfigFromEnv` parses `TANGO_DB_DSN` (default `sqlite://app.db`) with `db.ParseDSN`. | [configuration](guides/configuration.md) |
| `func BuildRegistry(config Config) (*Registry, error)` | Registers every `Config.InstalledApps` entry into a new `Registry`. Fails on `ErrDuplicateApp`. | |
| `func Check(config Config) error` | Runs registration, compiles the route tree, and enforces app-contributed checks; the logic behind `tango check`'s `-check` flag. | [app checks](guides/app-checks.md) |
| `func DispatchFlags(config Config, sqlDB *sql.DB, dialect db.Dialect, migrations []migration.Migration) (bool, error)` | Handles the stable app-side flags (`-check`, `-tango-dump-models`, `-tango-status`, `-migrate`, `-migrate -down`) and reports whether serving should stop. | [project structure](guides/project-structure.md) |
| `func Serve(config Config, sqlDB *sql.DB, dialect db.Dialect) error` | Builds the registry, runs registration, wires the `db.Store`, compiles routes, and serves. Blocks forever — delegates to `ServeContext(context.Background(), ...)`, so there is no caller-triggered shutdown path. `dialect` must match whatever `sqlDB` was opened with — pass the same value given to `DispatchFlags`. | [project structure](guides/project-structure.md), [application lifecycle](guides/application-lifecycle.md) |
| `func ServeContext(ctx context.Context, config Config, sqlDB *sql.DB, dialect db.Dialect, opts ...ServeOption) error` | Like `Serve`, but additionally starts every registered `Lifecycle` around the HTTP server's lifetime and stops gracefully when `ctx` is canceled: drains in-flight requests (force-closing, and still reporting the drain-timeout error, if the drain deadline is hit — `errors.Is(err, context.DeadlineExceeded)`), then stops every started `Lifecycle` in reverse order against one shared timeout budget. A `Start` failure, or cancellation while a `Start` is running (the Application context is canceled promptly so a cooperative `Start` can return; `ServeContext` always waits for it before proceeding, so no goroutine is leaked), rolls back already-started components and never starts the HTTP server. | [application lifecycle](guides/application-lifecycle.md) |
| `type ServeOption func(*serveConfig)`, `func WithShutdownTimeout(d time.Duration) ServeOption`, `func WithReadHeaderTimeout(d time.Duration) ServeOption`, `func WithLogger(*slog.Logger) ServeOption`, `func WithRecorder(observability.Recorder) ServeOption` | Configure shutdown budgets, the request-header timeout (default 10s, `0` disables it, negative is an error), and framework-owned logging/metrics. `WithLogger(nil)` is invalid; omitted logging uses `slog.Default()`. Metrics are no-op unless configured. | [application lifecycle](guides/application-lifecycle.md), [observability](guides/observability.md) |
| `func DumpModels(config Config) ([]migration.Model, error)` | Registered models in the dialect-agnostic shape `tango makemigrations` diffs against; backs `-tango-dump-models`. | [migrations](guides/migrations.md) |
| `func Status(ctx, config, sqlDB, dialect, migrations) ProjectStatus` | Registration/database/migration status as one payload; backs `-tango-status` and `tango tui`. | |
| `type Registry` | The app/model/route/admin registry apps register into. Key methods: `Register(App) error`, `RunRegistration() error`, `Models() *model.Registry`, `Routes() *RouteRegistry`, `Admin() *admin.Registry`, `SetStore`/`Store`, `Checks() []AppCheck`, `RegisterLifecycle(Lifecycle) error`, `Lifecycles() []Lifecycle`. | |
| `type AppCheck struct{ Description string; Err error }`, `type Checker interface{ Checks() []AppCheck }` | Optional advisory checks an `App` may contribute. | [app checks](guides/app-checks.md) |
| `type Lifecycle struct{ Name string; Start, Stop func(context.Context) error }`, `ErrDuplicateLifecycle` | A named component with optional startup/shutdown hooks, registered via `Registry.RegisterLifecycle` inside `App.Register` — never started implicitly. At least one of `Start`/`Stop` must be set; `Name` must be non-empty and unique (`ErrDuplicateLifecycle`, checkable via `errors.Is`). | [application lifecycle](guides/application-lifecycle.md) |
| `type Middleware func(http.Handler) http.Handler`, `func Recoverer(...RecoveryOption) Middleware`, `func RequestID(...RequestIDOption) Middleware`, `func AccessLogger(...AccessLogOption) Middleware` | Standard Go middleware plus opt-in recovery, correlation IDs, and access logging. | [routing](guides/routing-and-reverse-lookup.md#middleware-vs-view-wrappers), [observability](guides/observability.md) |
| `func MaxBodySize(n int64) Middleware` | Caps request bodies at `n` bytes: `413` with `{"error":"request body too large"}`, before the View when `Content-Length` is too large, else when a View returns the read's `*http.MaxBytesError`. The most restrictive applicable limit wins; `n` must be positive. | [request body limits](guides/routing-and-reverse-lookup.md#request-body-limits) |
| `func RequestIDFromContext(context.Context) (string, bool)`, `func WithTrustedHeader(string) RequestIDOption` | Read the generated request ID or explicitly trust one validated inbound header. | [observability](guides/observability.md) |
| `EventViewError`, `EventPanic`, `EventAccessLog`, `EventJobFailed`, `EventRequestIDGenerationFailed` | Stable structured-log event names. | [observability](guides/observability.md) |
| `func Path(method, pattern string, view View, opts ...RouteOption) Route`, `func Name(name string) RouteOption`, `func Use(...Middleware) RouteOption`, `type URLs []Route` | Declaring routes, including optional route-scoped middleware. | [routing](guides/routing-and-reverse-lookup.md) |
| `func (*RouteRegistry) Include(prefix string, routes []Route, opts ...IncludeOption) error`, `func WithMiddleware(...Middleware) IncludeOption` | Mounting routes under a namespace prefix, including optional group-scoped middleware. | [routing](guides/routing-and-reverse-lookup.md) |
| `func (*RouteRegistry) Handler() (http.Handler, error)`, `func (*RouteRegistry) Reverser() (Reverser, error)` | Compiling the route tree for serving / reverse lookup. Both require `RunRegistration` to have completed. | [routing](guides/routing-and-reverse-lookup.md) |
| `type Params map[string]string`, `type Reverser interface{ Reverse(name string, params Params) (string, error) }` | Reverse URL lookup. | [routing](guides/routing-and-reverse-lookup.md) |
| `type Context`, `type View func(*Context) error` | The request/response wrapper every view receives: `Param`, `Query`, `Bind`, `JSON`, `Redirect`, `HTML`, `Request`, `ResponseWriter`, `Context`, `Logger`. `Logger` is request-scoped and enriched with route, method, and request ID when present. | [Context and binding](guides/context-and-binding.md), [observability](guides/observability.md) |
| `ErrDuplicateApp`, `ErrMalformedPattern`, `ErrDuplicateRouteName`, `ErrRegistrationNotComplete`, `ErrUnknownRouteName`, `ErrMissingParam` | Sentinel errors, checkable via `errors.Is`. | |

## `observability` (`github.com/angvp/tango/observability`)

| Symbol | What it's for |
|---|---|
| `type Recorder interface`, `type NopRecorder struct{}` | Backend-neutral counter/histogram recording. Implementations must be concurrency-safe; `NopRecorder`'s zero value is ready to use. |
| `MetricHTTPRequestDuration`, `MetricSchedulerJobInvocations`, `MetricRealtimeRoomEvents` | Stable adapter-facing metric names. Durations use seconds; labels use bounded vocabularies. |

See [structured logging and observability](guides/observability.md).

## `model` (`github.com/angvp/tango/model`)

| Symbol | What it's for |
|---|---|
| `type ModelMeta struct{ Name string; App string; Type reflect.Type; Fields []FieldMeta }` | Introspected metadata for one registered model. `Name` is the exact Go type name; `App` records which app registered it. |
| `type FieldMeta struct{ Name string; Type reflect.Type; PrimaryKey, Unique, Indexed, Editable bool; ForeignKey string; MaxLength int }` | One field's metadata, derived from its type and `tango` tag. `ForeignKey` is the target model name from `tango:"fk=X"`, or `""`. `MaxLength` is the limit from `tango:"varchar=n"` in runes, or `0` for an unbounded string and every other kind. |
| `type Registry`, `func (*Registry) Register(value any) error`, `func (*Registry) Get(name string) (ModelMeta, bool)`, `func (*Registry) All() []ModelMeta`, `func (*Registry) ValidateForeignKeys() error` | The model registry, reached via `tango.Registry.Models()`. `ValidateForeignKeys` checks every `fk=` target is registered; `tango.Check` calls it automatically. |
| `ErrDuplicateModel`, `ErrNoPrimaryKey`, `ErrMultiplePrimaryKeys`, `ErrEmbeddedField`, `ErrUnsupportedField`, `ErrUnknownForeignKeyTarget`, `ErrInvalidFieldTag` | Sentinel errors from `Register`/`ValidateForeignKeys`. `ErrInvalidFieldTag` is a malformed `varchar=n` or `text` tag, named by model and field. |
| `MaxVarcharLength` | The largest `varchar=n` a tag may declare, 10,485,760 (PostgreSQL's own limit). |

See [models and tags](guides/models-and-tags.md) for the full tag/field-kind reference, and [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md) for `fk=`.

## `db` (`github.com/angvp/tango/db`)

| Symbol | What it's for |
|---|---|
| `type Dialect`, `SQLite`, `Postgres` | Which SQL dialect a `Store` generates for. |
| `func NewStore(sqlDB *sql.DB, dialect Dialect) *Store` | Construct a `Store`. |
| `func (*Store) Create/Update(ctx, meta, dest) error`, `func (*Store) Get(ctx, meta, ...) error`, `func (*Store) List(ctx, meta, Query, dest) error` | Metadata-driven CRUD. When `UseModels` has been called, `Create`/`Update` also validate every set foreign key field against an existing related row (referential-integrity validation), failing with `ErrInvalidForeignKey`. See [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md). |
| `func (*Store) Delete(ctx, meta, pk) error`, `func (*Store) UseModels(models *model.Registry)` | Deletes the matching row; when `UseModels` has been called (`tango.Registry.SetStore` does this automatically), also cascades — deleting every row of every other registered model that references it via a foreign key field first, recursively, in one transaction. See [relationships and admin foreign keys](guides/relationships-and-admin-foreign-keys.md). |
| `type Query struct{ Where, Any []Condition; Limit, Offset int; OrderBy []string }` | Single-model filtering, pagination, and ordering for `List`; `Where` uses AND, `Any` is one OR group. |
| `type Condition struct{ Field string; Op Op; Value any }`, `type Op`, `OpEq`, `OpNe`, `OpGt`, `OpGte`, `OpLt`, `OpLte`, `OpLike` | Validated comparisons in `Query`; see the [persistence guide](guides/persistence-crud-and-raw-sql.md) for field kinds, SQL patterns, and `nil` handling. |
| `func (*Store) Count(ctx, meta, Query) (int, error)` | Count rows matching `Where` and `Any`, ignoring pagination and ordering. |
| `func (*Store) Query/QueryRow(ctx, dest, sql string, args ...any) error` | Raw-SQL escape hatches, reusing CRUD's scan-into-`dest` machinery — where `Store` stops and hand-written SQL begins for joins, aggregates, or cross-model filtering. |
| `func ColumnName(name string) string` | The snake_case table/column name derivation, exposed for anything that needs to match it (e.g. building raw SQL by hand). |
| `type DSN struct { Dialect Dialect; Driver, Source string }` | A parsed DSN: the dialect for `NewStore`, and the driver name and data source name for `sql.Open`. |
| `func ParseDSN(dsn string) (DSN, error)` | Turns a scheme-qualified DSN (`sqlite://app.db`, `sqlite:///abs/path.db`, `sqlite://:memory:`, `postgres://…`) into a `DSN`; any other form is an error. Applies `SQLiteForeignKeysDSN` for SQLite and a 5-second busy timeout unless the DSN sets one. Never registers a driver. See [configuration](guides/configuration.md#database-env-helpers). |
| `func SQLiteForeignKeysDSN(dsn string) string` | Appends the SQLite driver's `_foreign_keys=on` DSN parameter, so foreign key constraints are actually enforced on connections opened with the result. |
| `ErrNotFound` | Sentinel for a missing row on `Get`/`Update`/`Delete`. |
| `ErrValueTooLong`, `type ValueTooLongError struct{ Model, Field string; Max, Got int }` | `Create`/`Update` refuse a bounded string longer than its `varchar=n` limit, before any SQL. The error matches `ErrValueTooLong`; `Max` is the limit and `Got` the length, both in runes. Raw SQL is not checked. |
| `ErrInvalidForeignKey` | Sentinel for referential-integrity validation failing on `Create`/`Update` — a set foreign key field references a row that doesn't exist. Distinct from `model.ErrUnknownForeignKeyTarget`, which validates the related *model*, not a specific row. |

See [persistence CRUD and raw SQL](guides/persistence-crud-and-raw-sql.md) and [SQLite/PostgreSQL setup](guides/sqlite-and-postgresql-setup.md).

## `auth` (`github.com/angvp/tango/auth`)

| Symbol | What it's for |
|---|---|
| `func HashPassword(password string) (string, error)`, `func VerifyPassword(hash, password string) bool` | Thin bcrypt helpers for application-owned user models. |
| `func ValidateSessionModel(sessionMeta model.ModelMeta) error` | Checks auth's fixed session-model convention: primary key, `Token string`, `UserID` tagged as a foreign key, and `ExpiresAt time.Time`. |
| `func CreateSession(ctx, store, sessionMeta, userID, duration) (token, expiresAt, error)` | Creates a crypto-random, cookie-safe session token row for an app-owned session model. |
| `func SessionUser(ctx, store, sessionMeta, token) (userID any, ok bool, err error)` | Returns the stored user id for a valid, unexpired session token. |
| `func DeleteSession(ctx, store, sessionMeta, token) error` | Deletes session rows matching a token; missing tokens are a no-op. |
| `func CurrentUserID(ctx *tango.Context, store, sessionMeta, cookieName) (userID any, ok bool, err error)` | Reads a request cookie and resolves the current session's user id without hydrating the user model. |
| `func RequireLogin(store, sessionMeta, cookieName, loginPath, allowedPrefix, next) tango.View` | Wraps a `tango.View`, redirecting unauthenticated requests to `loginPath` with a `next` value validated against `allowedPrefix`. |
| `func DeriveCSRFToken(secret string) string`, `func VerifyCSRFToken(submitted, secret string) bool` | Form CSRF helpers for application login/logout forms. |
| `func SafeRedirect(raw, allowedPrefix, fallback string) string`, `func IsSafeRedirect(raw, allowedPrefix string) bool` | Same-app redirect validation helpers for application login flows. |
| `ErrInvalidSessionModel` | Sentinel for an app session model that does not match auth's fixed convention. |

See [application auth](guides/application-auth.md). This package is primitives-only: no default `User` model, no signup view, no login view, and no sharing with admin auth.

## `auth/jwt` (`github.com/angvp/tango/auth/jwt`)

| Symbol | What it's for |
|---|---|
| `type Claims struct{ Subject, Issuer, Audience string; IssuedAt, ExpiresAt time.Time }` | tanGO-owned, registered-claims-only decoded token payload. `Subject` is opaque application identity. |
| `type Key struct{ ID string; Secret []byte }` | One HS256 key identified by `kid`; secrets must contain at least `MinimumSecretBytes` bytes. |
| `func NewService(active Key, verificationKeys []Key, issuer, audience string, opts ...ServiceOption) (*Service, error)` | Constructs an immutable issuer/verifier. The active key signs and verifies; retired keys verify only. |
| `func WithMaxTTL(time.Duration) ServiceOption`, `func WithClockSkew(time.Duration) ServiceOption` | Configure issuance lifetime limits and up to five minutes of verification leeway. Defaults are `DefaultMaxTTL` (24 hours) and `DefaultClockSkew` (zero). |
| `func (*Service) Issue(subject string, ttl time.Duration) (string, error)`, `func (*Service) Verify(token string) (Claims, error)` | Issue an HS256 token or validate its algorithm, key, signature, issuer, audience, and timestamps without a database lookup. |
| `type Extractor func(*http.Request) (string, error)`, `BearerToken`, `QueryToken(name)` | Read a token from the standard bearer header or, when necessary, a named query parameter. |
| `func (*Service) Middleware(Extractor) tango.Middleware` | Optional-auth HTTP middleware: missing tokens pass; supplied invalid/expired tokens return a generic JSON 401; valid claims enter the request context. |
| `func FromContext(context.Context) (Claims, bool)`, `func Require(tango.View) tango.View` | Read verified claims and require them for a view. `Require` always rejects if middleware did not install claims first. |
| `ErrMissingToken`, `ErrInvalidToken`, `ErrExpiredToken` | Sentinel errors checkable with `errors.Is`; wire responses never reveal the distinction. |

See [JWT authentication](guides/jwt-auth.md) and the runnable [`examples/jwt-api`](../examples/jwt-api).

## `realtime` (`github.com/angvp/tango/realtime`)

| Symbol | What it's for |
|---|---|
| `type Principal struct{ UserID string }`, `type Peer interface{ Send(context.Context, []byte) error; Close() error }` | The stable actor identity and one live, replaceable transport connection for it. `Peer` implementations must be comparable (a pointer type); `Join` rejects a nil interface, a typed-nil pointer inside a non-nil `Peer` interface, or a non-comparable dynamic type, all with `ErrInvalidPeer`. |
| `type EventKind`, `type Event struct{ Kind EventKind; RoomID string; Principal Principal; Timer string; Payload []byte }` | `EventAction`/`EventTimer`/`EventJoin`/`EventLeave`. `Payload` is opaque — never parsed by `realtime` itself, and always copied before outbound delivery, so mutating a caller's buffer after `Dispatch`/`DispatchPeer` returns is safe. |
| `type Logic interface{ Handle(*RoomContext, Event) error; Snapshot(*RoomContext, Principal) ([]byte, error) }`, `type Factory func(roomID string) Logic` | Host-supplied domain behavior for one room; one `Hub` always uses the same `Factory`. |
| `type RoomContext` with `Broadcast`, `BroadcastExcept(userID string, ...)`, `SendUser(userID string, ...)`, `ResetTimer(name string, duration time.Duration) error` | Valid only inside one `Handle`/`Snapshot` call, on that room's own event-loop goroutine. |
| `type Options struct{ ReconnectWindow time.Duration; PeerQueue, RoomQueue int; Recorder observability.Recorder }` | Defaults: `DefaultReconnectWindow` (30s), `DefaultPeerQueue` (16), `DefaultRoomQueue` (64), and no-op metrics. A negative numeric field is rejected at `NewHub`. |
| `type Coordinator interface{ Join, Leave, DispatchPeer }`, `func NewHub(Factory, Options) (*Hub, error)` | `*Hub` implements `Coordinator`. `Coordinator` deliberately excludes `Hub.Dispatch` — a transport adapter must always act through `DispatchPeer`, bound to its own live connection. |
| `func (*Hub) Join(context.Context, roomID string, Principal, Peer) error`, `func (*Hub) Leave(...) error` | Block until the room's single-owner event loop has actually applied the operation *and* run `Logic.Handle` for the corresponding `EventJoin`/`EventLeave` — a Handle error there is returned to the caller, but membership/delivery already applied is never rolled back. A stale `Leave` (a `Peer` no longer current) is an idempotent no-op that emits no `EventLeave`. |
| `func (*Hub) Dispatch(context.Context, Event) error` | Host/bot-facing: delivers into `Logic.Handle` without requiring membership. Requires `Event.Kind == EventAction`. Not part of `Coordinator`. |
| `func (*Hub) DispatchPeer(context.Context, Event, Peer) error` | Transport-facing: like `Dispatch`, but only succeeds if `peer` is exactly the currently installed connection for `Event.Principal` in that room — a stale, replaced, overflow-removed, or unknown peer gets `ErrStalePeer` before `Logic.Handle` ever runs. What `realtime/websocket.View` calls. |
| `func (*Hub) SendUser(ctx context.Context, userID string, payload []byte) error` | Delivers to `userID`'s live peer in every room the Hub currently tracks where they have one. |
| `func (*Hub) Close(ctx context.Context) error` | Idempotent: rejects new `Join`/`Dispatch`/`DispatchPeer` with `ErrClosed`, stops timers, closes peers, waits for every room loop to exit (or `ctx` to be canceled). An op already queued, or blocked trying to enqueue, when `Close` began also resolves to `ErrClosed` — never `ErrRoomNotFound`, which stays reserved for an actual room eviction. |
| `ErrRoomNotFound`, `ErrClosed`, `ErrStalePeer`, `ErrInvalidPeer` | Sentinel errors checkable with `errors.Is`. |

See [realtime and WebSockets](guides/realtime-websockets.md), [ADR 0028](adr/0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md), and the runnable [`examples/realtime-chat`](../examples/realtime-chat).

## `realtime/websocket` (`github.com/angvp/tango/realtime/websocket`)

| Symbol | What it's for |
|---|---|
| `type Authenticate func(*http.Request) (realtime.Principal, error)` | Resolves the requesting `Principal` before any WebSocket upgrade is attempted; a failure never touches the handshake. |
| `func View(coordinator realtime.Coordinator, authenticate Authenticate, roomID func(*tango.Context) (string, error)) tango.View` | Mount as an ordinary route. Blocks on `coordinator.Join` before starting the read loop; dispatches every inbound message through `coordinator.DispatchPeer`, carrying its own connection, never the unrestricted `Hub.Dispatch`; calls `Leave` exactly once when the connection ends, including a best-effort `Leave` after a failed `Join` (which has no rollback and can leave membership installed). |
| `DefaultMaxMessageSize` | 32 KiB inbound message cap; an oversized frame closes the connection rather than being buffered. |

Wraps `github.com/coder/websocket`; no third-party type appears in this package's own public API. See [realtime and WebSockets](guides/realtime-websockets.md).

## `ratelimit` (`github.com/angvp/tango/ratelimit`)

| Symbol | What it's for |
|---|---|
| `type Decision struct{ Allowed bool; Limit, Remaining int; RetryAfter time.Duration }` | The outcome of one `Limiter.Take` call — enough to build custom headers/body without reaching into `Limiter`'s internal state. |
| `type Options struct{ Limit int; Refill time.Duration; Clock func() time.Time }`, `func NewLimiter(Options) (*Limiter, error)` | Configures a token bucket's capacity/refill rate; `Limit`/`Refill` must be positive. `Clock` defaults to `time.Now`, injectable for deterministic tests. |
| `func (*Limiter) Take(ctx context.Context, key string, now time.Time, cost int) (Decision, error)` | Concrete, in-memory, per-key token bucket, safe for concurrent use. A key seen for the first time starts full (allows an initial burst up to `Limit`). `cost` must be positive and must not exceed `Limit` — a request that could never succeed is rejected outright, not given a finite `RetryAfter`. No storage interface — see [ADR 0029](adr/0029-ratelimit-is-a-concrete-token-bucket.md). Opportunistically evicts a key's bucket once it's been idle long enough to have fully refilled anyway (`Limit * Refill`), so long-running processes with many distinct keys don't retain them forever — this never changes any key's observed rate-limit behavior. |
| `type KeyFunc func(*http.Request) (string, error)`, `func RemoteIPKey(trustedProxies ...*net.IPNet) KeyFunc` | Caller-supplied key extraction — `ratelimit` never imports `auth`/`auth/jwt`/`accounts`. `RemoteIPKey` uses only `RemoteAddr` by default; with trusted CIDRs it reads `X-Forwarded-For` from the right, skipping trusted proxies, and only from an immediate peer inside a trusted CIDR. `X-Real-IP` is never read. |
| `type LimitedHandler func(http.ResponseWriter, *http.Request, Decision)`, `type ErrorHandler func(http.ResponseWriter, *http.Request, error)` | Respond to a rejected `Decision`, or to a `KeyFunc` failure — kept distinct; an extraction failure is never treated as "over budget." |
| `func Middleware(limiter *Limiter, key KeyFunc, opts ...MiddlewareOption) tango.Middleware`, `WithLimitedHandler`, `WithErrorHandler`, `WithCost(int)` | Ordinary `tango.Middleware` composition, usable at any attachment tier and on a WebSocket upgrade route. Default rejection: `429`, JSON, `Retry-After` (refill-derived, rounded up to whole seconds), `{"error":"rate limit exceeded"}`. Default extraction failure: `400`, JSON, `{"error":"rate limit key extraction failed"}`. |

Independent of `admin`/`accounts`' existing failed-login-attempt limiter, which counts authentication failures in a sliding window rather than every request — the two are not merged. See [rate limiting](guides/rate-limiting.md) and the runnable example wired into [`examples/notes-starter`](../examples/notes-starter).

## `mail` (`github.com/angvp/tango/mail`)

| Symbol | What it's for |
|---|---|
| `type Sender interface{ Send(ctx context.Context, message Message) error }` | Delivers one message. `Send` is synchronous and truthful: `nil` only once the message is handed on. An interface because real alternatives exist from day one: SMTP, a provider's HTTP API, and a test double. See [ADR 0044](adr/0044-mail-is-a-sender-interface-with-tls-required-smtp.md). |
| `type Message struct{ From, To, Subject, Text string; Attachments []Attachment }` | One plain-text email to one recipient. `From` and `To` are single addresses, optionally with a display name. |
| `type Attachment struct{ Filename, ContentType string; Data []byte }` | One in-memory file attached to a `Message`. A message with attachments is sent as `multipart/mixed`, with base64 attachments; without, it stays plain text. An empty `ContentType` is sent as `application/octet-stream`; empty `Data` is valid. |
| `var ErrInvalidMessage` | A message no sender will send: a missing or malformed address, or a line break in a header value (header injection). Nothing is sent. |
| `func WriterSender(w io.Writer, opts ...Option) Sender` | Writes each message to `w` in the exact wire format SMTP would send, for development. Nothing falls back to it automatically; don't point it at production logs, since messages carry live links. |
| `var ErrInvalidAttachment` | An attachment with an empty filename, a control character in its filename, or a malformed content type. Also matches `ErrInvalidMessage`. |
| `var ErrMessageTooLarge`, `const DefaultMaxAttachmentSize`, `func WithMaxAttachmentSize(n int64) Option` | A message whose attachments together exceed the limit, 10 MiB of raw bytes by default, fails with `ErrMessageTooLarge`, which doesn't match `ErrInvalidMessage`. Base64 makes attachments about a third larger on the wire; the limit bounds what is sent, not memory already allocated. `WithMaxAttachmentSize` panics unless `n` is positive. |
| `func NewSMTPSender(rawURL string, opts ...Option) (*SMTPSender, error)`, `type SMTPSender` | Delivers through an SMTP server, one connection per message. The scheme picks the security: `smtp://` requires STARTTLS (a server without it is refused before any credentials are sent), `smtps://` is TLS from the first byte, and `smtp+insecure://` is plaintext, accepted only for `localhost` or a loopback IP, never with credentials and only with an explicit port, for tools such as Mailpit. `smtp` and `smtps` default to ports 587 and 465. Credentials in the URL are sent with AUTH PLAIN. Errors never contain the URL, credentials, recipient or message; a server's refusal is reported by stage and reply code only, any other failure by server address and stage only, and `errors.Is` still matches the cause. See [ADR 0044](adr/0044-mail-is-a-sender-interface-with-tls-required-smtp.md). |
| `func SMTPSenderFromEnv(opts ...Option) (*SMTPSender, error)`, `const SMTPURLEnv`, `var ErrNotConfigured` | `NewSMTPSender` for `TANGO_SMTP_URL`, or `ErrNotConfigured` when it's unset or empty, so `main` decides what "no mail" means. |
| `const DefaultSMTPTimeout` | One SMTP exchange is bounded by `Send`'s context deadline, or 30 seconds when it has none; cancelling the context ends it at once. |
| `type Option` | Configures a sender. |

## `mail/mailtest` (`github.com/angvp/tango/mail/mailtest`)

| Symbol | What it's for |
|---|---|
| `type Sender struct{ Err error }`, `func (*Sender) Send(ctx, mail.Message) error`, `func (*Sender) Messages() []mail.Message` | A `mail.Sender` for tests that records messages instead of delivering them. It rejects invalid messages exactly as real senders do; with `Err` set, every `Send` returns it. Safe for concurrent use. |

## `accounts` (`github.com/angvp/tango/accounts`)

| Symbol | What it's for |
|---|---|
| `func New(store *db.Store, opts ...Option) tango.App` | Constructs the accounts application, installable via `InstalledApps` like any reusable app. Mounts a fixed `/accounts/register/`, `/accounts/login/`, `/accounts/logout/`. `New(store)` with no options defaults to signup enabled, a 30-day session in a `tango_account_session` cookie. |
| `type Account struct{ ID, Email, PasswordHash, Active, CreatedAt, EmailVerifiedAt }`, `type AccountSession struct{ ID, Token, UserID, ExpiresAt }` | The Application user/session model pair `New` registers. Deliberately no permission-shaped field on `Account` — see [the accounts guide](guides/accounts.md). `EmailVerifiedAt` is when the owner proved they receive mail at `Email`; the zero time means unverified, and it gates nothing by itself. |
| `type AccountToken struct{ ID, TokenHash, AccountID, Purpose, AddressHash, ExpiresAt }`, `type TokenPurpose`, `PurposePasswordReset`, `PurposeEmailVerification` | One outstanding emailed token, registered by `New` with the other models. Only hashes are stored: of the token, and of the address it was sent to. |
| `func WithTrustedProxies(...*net.IPNet) Option` | Names the reverse proxies in front of the app so the failed-attempt limiters (login, registration, password reset, verification resend) count the forwarded client address instead of the proxy's. Without it, no forwarded header is read. |
| `func WithSignupDisabled() Option` | Closes self-service registration; `/accounts/register/` still returns a clear closed-registration response, never 404. Signup is enabled by default. |
| `func WithSessionDuration(d time.Duration) Option` | Overrides the default 30-day session duration. |
| `func WithSessionCookieName(name string) Option`, `DefaultSessionCookieName` | Overrides (or names) the session cookie, default `"tango_account_session"`. |
| `func WithMail(config MailConfig) Option`, `type MailConfig struct{ Sender mail.Sender; From, BaseURL string; Logger *slog.Logger }` | Turns on password reset and email verification. `BaseURL` is the Public base URL links are built on: an absolute `https://` origin, or `http://` for localhost; links never come from the request's `Host`. Validated at registration, so `-check` fails on a bad configuration. Emails go through an in-memory outbox of 100 drained by one worker registered as a Lifecycle component; a full outbox drops the email. Without `WithMail`, the mail routes aren't mounted. See [ADR 0045](adr/0045-accounts-reset-and-verification-never-reveal-an-account.md). |
| `GET`/`POST /accounts/password-reset/` (with `WithMail`) | Asks for a reset link. Every syntactically valid address gets the same response, whether or not it has an account, is active, or is in its cooldown: the outbox, not the request, looks the account up, and only an active account is emailed. An empty or malformed address is a `400` form error. One email per address every five minutes (a dropped email gives its cooldown back), and five requests per client IP a minute. The link lasts one hour. |
| `GET`/`POST /accounts/password-reset/confirm/?token=…` (with `WithMail`) | GET shows the new-password form and never uses the token, so mail scanners following the link change nothing; POST sets the password under registration's rules. Success deletes every token and session of the account, sets `EmailVerifiedAt` if empty, and redirects to the login page. Both send `Referrer-Policy: no-referrer` and `Cache-Control: no-store`. An unknown, expired, used, replaced or wrong-purpose token, an inactive account, or an account whose email changed since the link was sent all get one `400` page. |
| `GET`/`POST /accounts/verify/?token=…`, `POST /accounts/verify/resend/` (with `WithMail`) | Registration queues a verification email; its 24-hour link shows a "Confirm my email" button on GET, and only POST sets `EmailVerifiedAt`. The same headers and single invalid-link page as password reset. Resend is for a logged-in account, needs the CSRF token the `RequireVerified` page carries, allows five requests per client IP a minute, and shares the five-minute per-address cooldown. |
| `func RequireVerified(store *db.Store, cookieName, loginPath string, next tango.View) tango.View` | Like `RequireLogin`, and also requires a Verified email: a logged-in but unverified account gets a `403` HTML page with a button to send a new link (which needs `WithMail`). Verification never stops a login; this guard is the only thing that acts on it. |
| `const EventMailFailed`, `const EventMailDropped` | `"tango.accounts.mail_failed"` (attributes `purpose`, `error`) and `"tango.accounts.mail_dropped"` (`purpose`). The error is redacted: never the address, the link or the message. |
| `func RequireLogin(store *db.Store, cookieName, loginPath string, next tango.View) tango.View` | View wrapper for a host's own routes, mirroring `auth.RequireLogin`'s shape. `Active` is re-checked on every request through an already-valid session, not just at login. |
| `func CurrentAccountID(ctx *tango.Context, store *db.Store, cookieName string) (int64, bool, error)`, `func CurrentAccount(...) (Account, bool, error)` | Thin, `Active`-aware sugar over `auth.CurrentUserID`, for a host's own Views. Not a route — there is no `/accounts/me/` page. |

See [the accounts guide](guides/accounts.md). Built entirely by composing `auth`'s primitives — no new primitives added to `auth` itself; HTML-only, no JSON auth endpoints (only a rejected CSRF token's `403` and rate limiting's `429` answer with a JSON `{"error": …}`), no CLI, no template-override hook.

## `i18n` (`github.com/angvp/tango/i18n`)

| Symbol | What it's for |
|---|---|
| `func T(ctx context.Context, key, fallback string, args ...any) string` | Looks up a symbolic translation key using the locale carried by `ctx`, falling back to the inline English fallback. If `args` are present, formats the chosen string with `fmt.Sprintf`. |
| `func RegisterCatalog(locale string, catalog map[string]string)` | Merges a Go-source catalog into the named locale. Multiple calls for one locale merge per key; later values win on key collision. |
| `type LocaleResolver func(*http.Request) string` | Host-provided per-request locale selection hook. |
| `func DefaultLocaleResolver(r *http.Request) string` | Parses `Accept-Language` and returns the best registered locale using tanGO's simplified exact-then-base-language matching. |
| `func Middleware(resolver LocaleResolver) tango.Middleware` | Resolves the locale once per request and stores it on the standard request `context.Context`; pass `nil` to use `DefaultLocaleResolver`. |
| `func WithLocale(ctx context.Context, locale string) context.Context`, `func Locale(ctx context.Context) string` | Low-level context helpers used by the middleware and useful in tests or custom middleware. |

See [i18n and l10n](guides/i18n.md). Admin is converted to call `i18n.T`, but no first-party app wires the middleware automatically — translation is always a host-level opt-in.

## `admin` (`github.com/angvp/tango/admin`)

| Symbol | What it's for |
|---|---|
| `func New(store *db.Store, opts ...Option) tango.App` | Constructs the admin app. Add it to `InstalledApps` after every app registering models with it. Mounts `/admin/`, `/admin`, `/admin/login/`, and `/admin/logout/` alongside each model's routes. Also registers `AdminUser`/`AdminSession` as ordinary models. `New(store)` with no options is unchanged. |
| `type AdminUser struct{ ID, Username, PasswordHash, Active, IsStaff, IsSuperuser, CreatedAt }`, `type AdminSession struct{ ID, Token, UserID, ExpiresAt }` | The Admin account and session models `New` registers. Never registered with the admin's own CRUD registry — manage accounts via the `tango admin` CLI, not the admin UI. `IsStaff` gates admin-panel access on an already-authenticated session (403 if false); `IsSuperuser` ships now but is currently inert. See [admin registration](guides/admin-registration.md#staff-and-superuser-access). |
| `func CreateAccount(ctx, store, username, password string, opts ...AccountOption) error`, `func ResetPassword/Deactivate(ctx, store, username, ...) error` | The account-lifecycle functions behind `tango admin create/resetpassword/deactivate` — the only code paths that ever write a password, always bcrypt-hashed. `CreateAccount` defaults to `IsStaff=true, IsSuperuser=true`; pass `WithoutStaff()`/`WithoutSuperuser()` to opt out. |
| `func GrantStaff/RevokeStaff/GrantSuperuser/RevokeSuperuser(ctx, store, username string) error` | The account-flag functions behind `tango admin grant-staff/revoke-staff/grant-superuser/revoke-superuser` — each changes exactly the one flag it names, leaving `Active`, the password, and the other flag untouched. |
| `func HandleCLI(ctx, store, args, stdin, stdout, stderr) (bool, error)` | Handles the `-tango-admin-create/-resetpassword/-deactivate/-grant-staff/-revoke-staff/-grant-superuser/-revoke-superuser` app-side flags (plus `create`'s `-tango-admin-no-staff`/`-tango-admin-no-superuser` opt-outs) a generated `main.go` calls alongside `tango.DispatchFlags`. |
| `type Options struct{ ListDisplay, Search, Ordering []string; Label string; Labels, HelpText map[string]string; ReadOnly, FieldOrder []string; Widgets map[string]Widget }` | Per-model admin configuration, validated against the model's actual fields at registration time. `Label` names the field shown wherever this model is displayed as a related object. `Labels`/`HelpText`/`ReadOnly`/`FieldOrder`/`Widgets` customize the create/edit form — see [admin registration](guides/admin-registration.md#customizing-form-fields). Best-effort, not covered by the compatibility promise — see [what is not covered](compatibility.md#what-is-not-covered). |
| `type Widget interface{ Render(FieldContext) template.HTML; Parse(FieldContext, FieldValues, reflect.Value) error }`, `type FieldContext struct{...}`, `type FieldValues interface{ Get, Has(string) ... }` | The field-widget contract set via `Options.Widgets`. `FieldContext` (field name, label, help text, value, read-only flag, related-model select options, optional related create URL) is passed to both methods, so a widget derives its own submitted form key(s) from `FieldContext.Name` identically on both sides. See [admin registration](guides/admin-registration.md#custom-field-widgets). Best-effort, like `Options` above. |
| `func Textarea() Widget` | tanGO's one built-in widget beyond the default five (text/number/date-time/checkbox/foreign-key-select) — a multi-line text input for a string field. |
| `type Branding struct{ Name, LogoURL string }`, `func WithBranding(Branding) Option` | The sidebar's only two customization slots — a brand name and logo — passed to `New`. Best-effort, like `Widget` above. See [admin registration](guides/admin-registration.md#branding). |
| `func WithTrustedProxies(...*net.IPNet) admin.Option` | Names the reverse proxies in front of the app so the login limiter counts the forwarded client address instead of the proxy's. Without it, no forwarded header is read. |
| `func WithMiddleware(...tango.Middleware) Option` | Wraps only admin's internally contributed routes, inside global `Config.Middleware` and before admin Views. |
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

`Step` is implemented by `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, `DropIndex`, `RenameColumn`, `RenameTable` and `AlterColumnType` — each a plain struct describing one dialect-agnostic schema operation. Treat these as "what generated code looks like," not a hand-authored API.

Stable runtime contract:

| Symbol | What it's for |
|---|---|
| `func ApplyPending(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate`. Runs pending migrations by name, each app's in sequence, and a migration referencing another app's table after the one creating it — see [migrations](guides/migrations.md#order-across-apps). |
| `func RollbackLast(ctx, sqlDB, dialect, migrations) error` | Backs `-migrate -down`. Returns `ErrNoAppliedMigrations` or `ErrIrreversibleMigration` for their respective cases. |
| `func AppliedMigrations(ctx, sqlDB) (map[MigrationKey]bool, error)` | Reads the applied migration keys from an existing `tango_migrations` table. |
| `type MigrationKey struct{ App, Name string }` | The app-scoped identity used by `tango_migrations`. |
| `func IsMissingTrackingTable(error) bool` | Lets status/checking code treat a missing `tango_migrations` table as "no migrations applied" without creating the table. |
| `type Migration` | The generated migration value passed to `ApplyPending` and `RollbackLast`. |

CLI-internal exported surface. It is [not covered](compatibility.md#what-is-not-covered) by the compatibility promise, except as the [Generated-file contract](compatibility.md#the-generated-file-contract) protects files `tango makemigrations` wrote:

| Symbol | What it's for |
|---|---|
| `type Step`, `type Column`, `CreateTable`, `DropTable`, `AddColumn`, `DropColumn`, `AlterColumnUnique`, `CreateIndex`, `DropIndex`, `RenameColumn`, `RenameTable`, `AlterColumnType` | Generated migration-file representation; don't hand-author these in application code. `Column.Length` is a `varchar` column's limit and `AlterColumnType.FromLength`/`ToLength` carry the limits of a type change; all three are `0` for every other type, and a generated file that omits them means what it always meant. `Column.Default` is a raw SQL literal (e.g. `"TRUE"`), not a typed Go value — when set, `AddColumn` (and only `AddColumn`) emits `NOT NULL DEFAULT <Default>` so pre-existing rows backfill instead of going `NULL`; `CreateTable` ignores it, since a freshly created table has no existing rows to backfill. `tango makemigrations` never produces one from a model's struct tags; it only ever survives on a migration where it was hand-set (e.g. admin's `IsStaff`/`IsSuperuser` columns). Not a general default-value system for model fields. |
| `type Model`, `func ModelsFromMeta(...)` | The `-tango-dump-models` JSON bridge used by `tango makemigrations`; `Model.Struct` and `Model.Fields` carry the Go names its errors use. |
| `func Replay(...)`, `type SchemaState`, `type TableState`, `type ColumnState` | Schema reconstruction for `tango makemigrations`. |
| `func Diff(...)`, `func DiffModels(..., renames ...Rename)`, `type Rename`, `ErrUnsupportedChange`, `ErrInvalidRename` | Diff engine behind `tango makemigrations`. A `Rename` is one `--rename` mapping (`Column` empty for a model); `ErrUnsupportedChange` names every change no step can express (a non-widening type change, a primary-key or foreign-key-target change, a default that can't convert), `ErrInvalidRename` every mapping that doesn't fit history and the models. Either way no migrations are returned. |
| `func ApplyStep(...)` | Shared DDL translator used by migration runners and tests; use `tango migrate` / `ApplyPending` instead. |

## `shell` (`github.com/angvp/tango/shell`)

The console behind `tango shell`. A project's `shell/main.go` calls `Run`; everything else in the package is internal. Importing it links the Yaegi interpreter, so a server's `main` package should not.

| Symbol | What it's for | See also |
|---|---|---|
| `func Run(ctx context.Context, config tango.Config, store *db.Store, args []string, opts Options) int` | Registers `config`'s apps against `store` without serving anything, then runs a shell session for `args` (`os.Args[1:]`) and returns the process exit code: `0` for a clean end or `--help`, `1` for the first error of a non-interactive session or a project that fails to boot, `2` for a command line it does not understand, `130` when Ctrl-C ends a running evaluation. Accepts `-c EXPR`, `--readonly` and `--help`. | [shell guide](guides/shell.md) |
| `type Options struct{ ReadOnly bool; Helpers map[string]any; DatabaseLabel string }` | `ReadOnly` makes every helper that writes fail (as does `--readonly`). `Helpers` are the project's own functions, called in the session as `project.Name`; each name must be an exported Go identifier, and only plain values (primitives, maps, slices, `error`) are supported across the boundary. `DatabaseLabel` is display-only text shown at startup, which the project derives from its database configuration after removing every credential; it is never a DSN. | [shell guide](guides/shell.md#your-own-helpers) |

Inside a session the helpers are `Models`, `Describe`, `Get`, `List`, `Count`, `Create`, `Update`, `Delete` and `Context`, plus `help()`, `exit()` and `quit()`; the [shell guide](guides/shell.md#the-helpers) lists their arguments.

## `testdb` (`github.com/angvp/tango/testdb`)

For tests only. The run's Test dialect comes from `TANGO_TEST_DSN`, with the same grammar as `TANGO_DB_DSN`: unset or `sqlite://:memory:` is in-memory SQLite, any other `sqlite://` path is a fresh SQLite file per test (the path itself is not used), and `postgres://…`/`postgresql://…` is a fresh schema per test, dropped when the test ends. See [ADR 0040](adr/0040-tests-run-once-per-test-dialect-chosen-by-one-scheme-driven-dsn.md).

| Symbol | What it's for |
|---|---|
| `func Open(t testing.TB) (*sql.DB, db.Dialect)` | A fresh database for this test, closed by `t.Cleanup`. An unsupported scheme or an unreachable Postgres fails the test; it never skips. |
| `func Store(t testing.TB) *db.Store` | `db.NewStore` over `Open(t)`. |
| `func Dialect() db.Dialect` | The run's Test dialect, for per-dialect raw SQL inside a test. Panics on an unsupported `TANGO_TEST_DSN`, with the message `Open` fails the test with. |
| `func SQLiteOnly(t testing.TB, reason string)`, `func PostgresOnly(t testing.TB, reason string)` | Skip the test on the other dialect, with a reason, so every exemption is explicit and greppable. |
