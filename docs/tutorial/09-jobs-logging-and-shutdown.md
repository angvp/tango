# Tutorial, part 9: jobs, logging, and graceful shutdown

Continuing from [part 8](08-live-feed.md), this part gets the board ready to run unattended. You'll add a job that cleans up on a schedule, switch the app to structured JSON logs with a line per request, and make it shut down gracefully — finishing requests in flight instead of dropping them when the process is stopped.

## A background job

Every login creates a row in `account_session`, and nothing ever deletes the ones that expire. They're harmless — an expired session never logs anyone in — but the table only grows. A job fixes that. Jobs are registered by an app, like routes:

```sh
tango newapp housekeeping
```

```go
// Package housekeeping runs the board's background maintenance jobs.
package housekeeping

import (
	"context"
	"log/slog"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

func New(store *db.Store) tango.App {
	return tango.NewApp("housekeeping", func(registry *tango.Registry) error {
		sessionMeta, _ := registry.Models().Get("AccountSession")
		return registry.RegisterJob(tango.Job{
			Name:     "prune-expired-sessions",
			Interval: time.Hour,
			Run: func(ctx context.Context) error {
				return pruneSessions(ctx, store, sessionMeta)
			},
		})
	})
}

// pruneSessions deletes login sessions that have expired. They're already
// useless — an expired session never logs anyone in — but nothing else
// removes them, so without this the table only grows.
func pruneSessions(ctx context.Context, store *db.Store, sessionMeta model.ModelMeta) error {
	var expired []accounts.AccountSession
	err := store.List(ctx, sessionMeta, db.Query{
		Where: []db.Condition{{Field: "ExpiresAt", Op: db.OpLt, Value: time.Now().UTC()}},
		Limit: 500, // a bounded batch per run; the next run picks up the rest
	}, &expired)
	if err != nil {
		return err
	}
	for _, session := range expired {
		if err := store.Delete(ctx, sessionMeta, session.ID); err != nil {
			return err
		}
	}
	if len(expired) > 0 {
		slog.InfoContext(ctx, "pruned expired sessions", "count", len(expired))
	}
	return nil
}
```

A `tango.Job` is a name, an interval, and a function. tanGO runs it on a ticker once the server starts: the first run is one interval after startup, not at startup, and if a run is still going when the next tick arrives, that tick is skipped rather than queued — a slow job can never pile up copies of itself. The `ctx` passed to `Run` is canceled when the app shuts down, and `store` methods honor it, so a run in progress stops promptly.

`pruneSessions` deletes a bounded batch per run with the same typed `List` and `Delete` you've used all along, so it works unchanged on SQLite and Postgres. If a run returns an error or panics, tanGO logs it (as a `tango.scheduler.job_failed` event) and the next run happens on schedule anyway.

Install it in `main.go`, anywhere after `accounts` (and import `"board/apps/housekeeping"`):

```go
feed.App(),
housekeeping.New(store),
admin.New(store),
```

To watch it work without waiting an hour, temporarily set `Interval: 5 * time.Second`, log in, and put an expired session in the table by hand with `sqlite3 app.db "UPDATE account_session SET expires_at = '2020-01-01 00:00:00'"`. Within a few seconds you'll see `pruned expired sessions` in the log. Set it back to `time.Hour` when you're done.

## Structured logs

tanGO logs with the standard library's `log/slog`, and doesn't need a logging library of its own. At the top of `run()` in `main.go`, create one JSON logger and make it the default:

```go
// One structured JSON logger for the whole app. slog.SetDefault makes
// it the logger for tanGO's middleware and for slog calls in your code.
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
slog.SetDefault(logger)
```

Then add three built-in middleware to `Config`, which run on every request:

```go
Middleware: []tango.Middleware{
	tango.RequestID(),
	tango.Recoverer(),
	tango.AccessLogger(),
},
```

- **`RequestID`** gives each request a random ID, returns it in an `X-Request-ID` response header, and adds it to every log line the request produces — including the ones you write with `ctx.Logger()` in a view. When a user reports an error, the ID from their response finds every related log line.
- **`Recoverer`** turns a panic in a view into a normal `500` response and a log entry, instead of a crashed connection.
- **`AccessLogger`** writes one line per request: route, method, status, duration.

Keep them in this order — request ID first, so everything after it can log the ID. Each request now produces a line like:

```json
{"time":"2026-09-27T20:58:33.068545-06:00","level":"INFO","msg":"tango.http.access","route":"/posts","method":"GET","request_id":"bf3e8b12fe7f3828cbcbf08e589e16c2","status":200,"duration_seconds":0.000594416}
```

Note that `route` is the route's *pattern* (`/posts/{id}`), not the actual URL, so logs group by endpoint and never contain IDs or query strings a user typed. JSON on stdout is what most hosting platforms and log collectors expect; for reading logs in a terminal during development, `slog.NewTextHandler` prints the same fields as `key=value` pairs.

## Graceful shutdown

Until now, `main.go` ended with `tango.Serve`, which runs forever: stopping the process drops whatever requests were in flight. Replace the end of `run()` with `tango.ServeContext`, and let an OS signal cancel its context:

```go
// Ctrl-C or SIGTERM (what `docker stop` and most process managers send)
// cancels ctx, and ServeContext shuts down gracefully.
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

logger.Info("listening", "addr", config.Addr)
err = tango.ServeContext(ctx, config, sqlDB, dialect,
	tango.WithLogger(logger),
	tango.WithShutdownTimeout(10*time.Second),
)
logger.Info("stopped", "clean", err == nil)
return err
```

(new imports: `log/slog`, `os/signal`, `syscall`; `context` and `time` are already there.) When you press Ctrl-C — or a deployment sends `SIGTERM` — `ServeContext`:

1. stops accepting new connections and waits for requests in flight to finish, for up to the shutdown timeout;
2. stops the job scheduler, letting a running job see its `ctx` canceled;
3. runs every registered lifecycle's `Stop` in reverse order — which is when part 8's `hub.Close` closes the open WebSocket connections;
4. returns, and `main` exits.

It returns `nil` when all of that finished cleanly, and an error if something didn't — for example, a request still running when the timeout ran out. tanGO never installs signal handlers itself: which signals mean "stop" is your program's decision, made here in `main`.

## Try it

```sh
go run .
```

The first line is now JSON:

```json
{"time":"...","level":"INFO","msg":"listening","addr":":8000"}
```

Load a few pages and watch an access line appear for each; `curl -i http://localhost:8000/posts/` shows the `X-Request-ID` header that matches its log line. Then press Ctrl-C:

```json
{"time":"...","level":"INFO","msg":"stopped","clean":true}
```

If a browser had the front page open, its live-feed socket was closed as part of that shutdown, rather than being cut off.

**Part 10** makes all of this safe to change: automated tests for the board, and what it takes to deploy it.

Continue: [Tutorial, part 10: testing and deployment](10-testing-and-deployment.md)
