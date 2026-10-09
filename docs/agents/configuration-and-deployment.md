# Agent Recipe: Configuration and Deployment

Use this when wiring how an app starts, where it listens, or how it runs in a container or on a hosting platform.

Canonical files: `config.go`, `serve.go`, `examples/board/main.go` and `examples/board/Dockerfile`. Human guides: `docs/guides/configuration.md` (including its Production defaults section) and `docs/tutorial/10-testing-and-deployment.md`.

## Rule

- Build `Config` with `tango.LoadConfigFromEnv(tango.WithPortFromEnv())`: the address is `TANGO_ADDR`, else the `PORT` a hosting platform sets, else `:8000`.
- Open the database from `TANGO_DB_DSN` with `tango.LoadDBConfigFromEnv()`; `tango.LoadEnvFile(".env")` loads local development values without overriding the real environment.
- Serve with `tango.ServeContext` under `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` so Ctrl-C and `docker stop` shut down gracefully. `ServeContext` bounds slow request headers at 10 seconds; change it only with `tango.WithReadHeaderTimeout`.
- Keep the scaffold's production defaults: `RequestID`, `Recoverer`, `AccessLogger`, `MaxBodySize(1 << 20)` and `MiddlewareScopeAll`.
- Before deploying, run the binary with `-check`, then `-migrate`.

Tiny shape:

```go
config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
```

## Don't

- Do not hardcode `Addr: ":8000"` or call `http.ListenAndServe` yourself; you lose `PORT`, the header timeout and graceful shutdown.
- Do not commit secrets to `.env` files used in production; set real environment variables.
- `tango admin create` and `resetpassword` take the password from `TANGO_ADMIN_PASSWORD` when it is set and non-empty (no prompt, never printed); otherwise they prompt on stdin without echo. The environment is visible to process-inspection tools, so prefer the platform's secret mechanism, and keep `.env` out of version control.
- `tango migrate` prints `applied N migrations: app/name, …` or `no pending migrations`.
- Do not run several processes against in-memory state (rate limiters, realtime rooms, the accounts outbox) and expect them to share it.

## Check

- Compare with `examples/board/main.go`.
- Run `docs/agents/checklist.md`.
