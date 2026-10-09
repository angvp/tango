# Tutorial, part 10: testing and deployment

Continuing from [part 9](09-jobs-logging-and-shutdown.md), this last part makes the board safe to change and ready to run somewhere other than your laptop. You'll write tests that drive the real app through its real routes, move every setting into the environment, switch to PostgreSQL, and package the whole thing as a small container image.

## One config for the app and its tests

Tests should exercise the same app `main` runs — same apps, same order, same middleware — not a hand-assembled lookalike. `tango newproject` already gave you the function for that, `project.Config`, which `main.go`, `shell/main.go` and your tests all call. The board's, in full:

```go
// project/project.go
// Config composes the application: its installed apps, middleware and
// ordinary configuration. It does not load the .env file or open a database:
// each process does that itself and passes the store in.
func Config(store *db.Store) tango.Config {
	// Config has no error to return, so a missing secret or a feed that
	// cannot start stops the process here, with the message, before
	// anything is served or opened.
	tokens, err := newTokenService()
	if err != nil {
		log.Fatal(err)
	}
	feed, err := live.NewFeed()
	if err != nil {
		log.Fatal(err)
	}

	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv()) // TANGO_ADDR, else PORT, else :8000
	config.InstalledApps = []tango.App{
		accounts.New(store),
		posts.New(store, tokens, feed),
		api.New(store, tokens),
		web.New(store, feed),
		feed.App(),
		housekeeping.New(store),
		admin.New(store),
	}
	config.Middleware = []tango.Middleware{
		tango.RequestID(),
		tango.Recoverer(),
		tango.AccessLogger(),
		tango.MaxBodySize(1 << 20),
	}
	config.MiddlewareScope = tango.MiddlewareScopeAll
	return config
}
```

`main.go` calls it with the store it opened (`config := project.Config(store)`), and so do the tests, so a new app you add to `InstalledApps` is in all three at once.

## Tests that drive the real app

Everything a test needs is a fresh database and the app's `http.Handler`. `main_test.go` builds both: a database from `testdb.Open`, with every migration applied, and the app compiled from `project.Config` exactly the way `ServeContext` does it:

```go
// main_test.go
package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
	"github.com/angvp/tango/testdb"

	"board/migrations"
	"board/project"
)

// testApp is the real application — same apps, routes, and middleware as
// main — on a fresh, fully migrated database.
type testApp struct {
	handler  http.Handler
	store    *db.Store
	registry *tango.Registry
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // keep test output quiet

	sqlDB, dialect := testdb.Open(t)
	if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(sqlDB, dialect)
	t.Setenv("BOARD_JWT_SECRET", strings.Repeat("s", jwt.MinimumSecretBytes))

	registry, err := tango.BuildRegistry(project.Config(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RunRegistration(); err != nil {
		t.Fatal(err)
	}
	registry.SetStore(store)
	handler, err := registry.Routes().Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{handler: handler, store: store, registry: registry}
}

// do sends one request through the app and returns the recorded response.
func (a *testApp) do(method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	return rec
}

// signUp creates an active account directly in the database.
func (a *testApp) signUp(t *testing.T, email, password string) {
	t.Helper()
	meta, _ := a.registry.Models().Get("Account")
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	account := accounts.Account{Email: email, PasswordHash: hash, Active: true, CreatedAt: time.Now().UTC()}
	if err := a.store.Create(t.Context(), meta, &account); err != nil {
		t.Fatal(err)
	}
}

// token logs in through the API and returns a bearer token.
func (a *testApp) token(t *testing.T, email, password string) string {
	t.Helper()
	rec := a.do("POST", "/api/token/", `{"email":"`+email+`","password":"`+password+`"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("token: status %d: %s", rec.Code, rec.Body)
	}
	var body struct{ Token string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Token
}
```

The tests set `BOARD_JWT_SECRET` themselves, so they need no `.env` file. `httptest.NewRecorder` captures a response without opening a network socket, so requests go straight through tanGO's routing, middleware, and views in microseconds. `testdb.Open` gives each test its own empty database — in-memory SQLite by default — and cleans it up when the test ends, so tests never see each other's data and can run in any order.

Now the tests themselves. Each one pins down a behavior from an earlier part — the kind of thing that's easy to break with an innocent-looking refactor:

```go
// main_test.go
func TestAppPassesChecks(t *testing.T) {
	app := newTestApp(t)
	if err := tango.Check(project.Config(app.store)); err != nil {
		t.Fatal(err)
	}
}

func TestTokenEndpoint(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")

	tests := []struct {
		name, body string
		want       int
	}{
		{"right password", `{"email":"ana@example.com","password":"correct horse battery"}`, http.StatusOK},
		{"email is case-insensitive", `{"email":"ANA@example.com","password":"correct horse battery"}`, http.StatusOK},
		{"wrong password", `{"email":"ana@example.com","password":"nope"}`, http.StatusUnauthorized},
		{"unknown email", `{"email":"bo@example.com","password":"nope"}`, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := app.do("POST", "/api/token/", tt.body, ""); rec.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestCreatingAPostNeedsAToken(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	token := app.token(t, "ana@example.com", "correct horse battery")

	if rec := app.do("POST", "/posts/", `{"title":"x"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("without a token: status = %d, want 401", rec.Code)
	}
	if rec := app.do("POST", "/posts/", `{"title":"x"}`, "not-a-token"); rec.Code != http.StatusUnauthorized {
		t.Errorf("with a bad token: status = %d, want 401", rec.Code)
	}

	rec := app.do("POST", "/posts/", `{"title":"Hello","AccountID":999}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("with a token: status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var post struct{ AccountID int64 }
	json.Unmarshal(rec.Body.Bytes(), &post)
	if post.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1 (from the token, not the body)", post.AccountID)
	}
}

func TestOnlyTheOwnerCanDeleteAPost(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	app.signUp(t, "bo@example.com", "another good password")
	ana := app.token(t, "ana@example.com", "correct horse battery")
	bo := app.token(t, "bo@example.com", "another good password")

	app.do("POST", "/posts/", `{"title":"Ana's post"}`, ana)
	app.do("POST", "/posts/1/comments/", `{"author":"bo","body":"Nice"}`, "")

	if rec := app.do("DELETE", "/posts/1/", "", bo); rec.Code != http.StatusForbidden {
		t.Errorf("someone else's post: status = %d, want 403", rec.Code)
	}
	if rec := app.do("DELETE", "/posts/1/", "", ana); rec.Code != http.StatusNoContent {
		t.Errorf("own post: status = %d, want 204", rec.Code)
	}
	if rec := app.do("GET", "/posts/1/", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", rec.Code)
	}
	if rec := app.do("GET", "/posts/1/comments/", "", ""); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("comments after delete = %s, want [] (they cascade)", rec.Body)
	}
}

func TestFrontPageEscapesTitles(t *testing.T) {
	app := newTestApp(t)
	app.signUp(t, "ana@example.com", "correct horse battery")
	token := app.token(t, "ana@example.com", "correct horse battery")
	app.do("POST", "/posts/", `{"title":"<script>alert(1)</script>"}`, token)

	rec := app.do("GET", "/", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<script>alert(1)") {
		t.Error("the title was rendered as HTML")
	}
	if !strings.Contains(rec.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the escaped title is missing")
	}
}
```

Run them:

```sh
go test ./...
```

The same tests run against PostgreSQL when `TANGO_TEST_DSN` names a server; `testdb` then gives each test a schema of its own and drops it afterwards. It reads the same scheme-qualified DSNs as `TANGO_DB_DSN` (see [PostgreSQL](#postgresql) below):

```sh
TANGO_TEST_DSN="postgres://board:board@localhost:5432/board?sslmode=disable" go test ./...
```

To see that they're worth having, break something on purpose: flip the ownership check in `deletePost` from `post.AccountID != account.ID` to `==` and run the tests again. `TestOnlyTheOwnerCanDeleteAPost` fails with `someone else's post: status = 204, want 403`.

`TestAppPassesChecks` runs the same validation as `go run . -check`: every route compiles, every foreign key points at a registered model, every template parses. Since the check is one of the tests, CI only needs two commands on every change:

```sh
go vet ./...
go test ./...
```

## Configuration comes from the environment

Every setting the board needs is now an environment variable, with sensible local defaults:

| Variable | What it sets | Default |
|---|---|---|
| `TANGO_ADDR` | The address to listen on | `:8000` |
| `TANGO_DB_DSN` | The database to connect to; its scheme picks SQLite or Postgres | `sqlite://app.db` |
| `BOARD_JWT_SECRET` | The API token signing secret (part 7) | none — required |

Locally, `.env` fills them in; in production, set them in the environment instead. Real environment variables always win over `.env`, and the app runs fine without a `.env` file at all.

## PostgreSQL

SQLite is a fine default: one file, nothing to install, and plenty for a small site on one server. When you want a database server — several app instances, managed backups, more concurrent writes — tanGO also supports PostgreSQL, and the database setup `tango newproject` generated at the top of `run()` already reads `TANGO_DB_DSN`:

```go
// main.go
dsn, err := tango.LoadDBConfigFromEnv()
if err != nil {
	return err
}

sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
if err != nil {
	return err
}
defer sqlDB.Close()

store := db.NewStore(sqlDB, dsn.Dialect)
```

and `dsn.Dialect` is what you've been passing to `tango.DispatchFlags` and `tango.ServeContext` all along. The DSN's scheme picks the database: `sqlite://app.db` (the default) is a file in the working directory, `sqlite:///var/data/app.db` an absolute path, and `postgres://…` a PostgreSQL server. `tango.LoadDBConfigFromEnv` hands back a `db.DSN` holding the matching `Dialect`, the `Driver` name for `sql.Open`, and the `Source` string the driver expects, with SQLite's foreign key enforcement already switched on. A DSN without a scheme, such as a bare `app.db`, is an error.

The Postgres driver needs one more import in `main.go` (and in `shell/main.go`, so `tango shell` can open the same database), next to the SQLite one:

```go
// main.go
_ "github.com/jackc/pgx/v5/stdlib"
_ "modernc.org/sqlite"
```

The migrations don't change. They're a list of typed steps — create this table, add that column — and tanGO turns each step into the right SQL for the database it's applied to, so the files `tango makemigrations` wrote against SQLite apply unchanged to Postgres. Everything else carries over too: `db.Store` writes the right SQL for each database, and the raw query from part 4 is plain SQL that both accept.

To try it, start a throwaway Postgres in Docker:

```sh
docker run -d --rm --name board-pg -p 5432:5432 \
  -e POSTGRES_USER=board -e POSTGRES_PASSWORD=board -e POSTGRES_DB=board \
  postgres:17-alpine

export TANGO_DB_DSN="postgres://board:board@localhost:5432/board?sslmode=disable"

go run . -migrate
go run . -tango-admin-create=admin
go run .
```

`-migrate` and `-tango-admin-create` are the flags `tango migrate` and `tango admin create` use under the hood. On a server you run them against the built binary, where the `tango` CLI isn't installed.

## Build it

```sh
go build -o board .
```

That's the whole deployable: one binary. Templates, the stylesheet, and every migration are compiled in with `go:embed` and Go code, so there's nothing else to copy next to it. For a container, a two-stage `Dockerfile` builds the binary in the Go image and copies only the binary into a minimal runtime image:

```dockerfile
# Build stage: compile one static binary. Templates and static files are
# embedded with go:embed, so the binary is everything the image needs.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/board .

# Runtime stage: a minimal image with no shell, running as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/board /board
EXPOSE 8000
ENTRYPOINT ["/board"]
```

and a `.dockerignore`, so local secrets and your development database never end up in the image:

```text
.env
app.db
board
```

`CGO_ENABLED=0` works because every dependency, including the SQLite driver, is pure Go. The result is an image of under 30 MB with no shell and no root user. Build it, apply migrations with a one-off container, then start the app:

```sh
docker build -t board .

docker run --rm --env-file prod.env board -migrate
docker run -d --name board -p 8000:8000 --env-file prod.env board
```

where `prod.env` sets `TANGO_DB_DSN` (with its `postgres://` or `sqlite://` scheme) and `BOARD_JWT_SECRET` for production. (Pointing a container at a database on your own machine? Use `host.docker.internal` instead of `localhost` in the DSN.) `docker stop board` sends `SIGTERM`, and the graceful shutdown from part 9 finishes in-flight requests before the container exits:

```json
{"level":"INFO","msg":"stopped","clean":true}
```

## Before you go live

- **Serve over HTTPS.** Put the app behind a proxy or load balancer that terminates TLS. `accounts` and the admin mark their cookies `Secure` when the visitor connected over HTTPS. Behind a proxy the app itself sees plain HTTP, so it goes by the proxy's `X-Forwarded-Proto` (or `Forwarded`) header; the common proxies and platforms send it by default. Make sure the proxy only accepts HTTPS from the outside.
- **Generate real secrets.** `BOARD_JWT_SECRET` should be long and random (`openssl rand -hex 32`), different in every environment, and never committed.
- **Tell the rate limiter about your proxy.** Behind a proxy, every request appears to come from the proxy's address. Pass its network to `ratelimit.RemoteIPKey` as described in part 7, or every visitor shares one limit. Your proxy must add the address it sees to `X-Forwarded-For`.
- **Run one instance, or know what that means.** Rate limits (part 7) and the live feed (part 8) live in memory, per process. With two instances behind a load balancer, each keeps its own counts and its own WebSocket rooms.
- **Back up the database.** For SQLite that's the `app.db` file — use `sqlite3 app.db ".backup backup.db"` rather than copying a file that's being written to. For Postgres, your provider's backups or `pg_dump`.
- **Read [limitations and compatibility](../limitations.md)**, which lists what tanGO deliberately doesn't do yet.

## What you've built

Over ten parts, the board grew from an empty directory into a complete application:

- JSON API routes, with bearer tokens, ownership checks, and rate limits (parts 1, 4, 7)
- Models linked by foreign keys, with cascading deletes and generated migrations (parts 2, 4, 6)
- An HTML admin for every model (part 3)
- Server-rendered pages built from route names, with sign-up, login, and CSRF-safe forms (parts 5, 6)
- A live front page over WebSockets (part 8)
- A scheduled cleanup job, structured logs, and graceful shutdown (part 9)
- Tests through the real app, and a 28 MB container image that runs on SQLite or Postgres (part 10)

Nothing happened by magic along the way. Every app is listed in `project/project.go`, every route is declared where its app registers, every table came from a migration you generated and read, and every setting comes from the environment. That explicitness is what tanGO is for.

From here, the [guides](../guides/) go deeper on each topic, the [API reference](../reference.md) lists everything tanGO offers, and [application architecture](../guides/application-architecture.md) covers how to grow an app like this one as it gets bigger.
