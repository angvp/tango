# Agent Recipe: Testing with `testdb`

Use this when writing tests for an app's Views, models or migrations.

Canonical files: `testdb/testdb.go`, `examples/board/main_test.go` and `examples/accounts-mail/main_test.go`. Human guides: `docs/guides/sqlite-and-postgresql-setup.md` and `docs/tutorial/10-testing-and-deployment.md`.

## Rule

- Get a fresh, isolated database per test with `testdb.Open(t)` (or `testdb.Store(t)`); `TANGO_TEST_DSN` picks SQLite, PostgreSQL or MySQL, so one suite runs on all three.
- Apply the app's real migrations with `migration.ApplyPending`, then build the app from the same config function `main` uses, and exercise it through `registry.Routes().Handler()` with `httptest`.
- Test email with `mailtest.Sender`, passed through that config function; no test-only flags.
- Start `registry.Lifecycles()` when the behaviour needs them (the accounts outbox, realtime hubs), and stop them in `t.Cleanup`.
- Mark a test that can run on only one dialect with `testdb.SQLiteOnly`, `testdb.PostgresOnly`, `testdb.MySQLOnly` or `testdb.NotMySQL` and a reason.

Tiny shape:

```go
sqlDB, dialect := testdb.Open(t)
if err := migration.ApplyPending(t.Context(), sqlDB, dialect, migrations.Migrations); err != nil {
	t.Fatal(err)
}
registry, err := tango.BuildRegistry(appConfig(db.NewStore(sqlDB, dialect)))
```

## Don't

- Do not share one database between tests, or open a hardcoded SQLite file.
- Do not test through a side channel (querying tables) when the HTTP behaviour shows the same thing.
- Do not skip a PostgreSQL or MySQL run silently; an unreachable server fails the test by design.

## Check

- SQLite CI runs the complete suite. Pull-request PostgreSQL and MySQL CI run the checked-in dialect-sensitive package manifest; the scheduled full matrix and release workflow run every package on all three dialects. When your change touches SQL, run the affected tests locally with both a `postgres://` and a `mysql://` `TANGO_TEST_DSN`.
- Run `docs/agents/checklist.md`.
