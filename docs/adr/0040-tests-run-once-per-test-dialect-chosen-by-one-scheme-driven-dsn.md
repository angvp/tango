# Tests run once per Test dialect, chosen by one scheme-driven DSN

Until now the suite ran on in-memory SQLite, and five test files had Postgres twins that ran only when `TANGO_TEST_POSTGRES_DSN` was set and skipped silently otherwise. Nothing ran them in CI, and SQL generated outside those five files was never checked against Postgres at all.

The whole suite now runs once per Test dialect. One variable, `TANGO_TEST_DSN`, picks the dialect by its scheme, read through `db.ParseDSN`, the same parser and grammar an app uses for `TANGO_DB_DSN`: unset or `sqlite://:memory:` is in-memory SQLite, any other `sqlite://` path is a fresh SQLite file per test under `t.TempDir()`, and `postgres://…`/`postgresql://…` is PostgreSQL. Anything else fails. Tests get their database from a public `testdb` package (`Open`, `Store`, `Dialect`, `SQLiteOnly`, `PostgresOnly`); it is public because `examples/board` is a separate module and apps built on tanGO want the same thing for their own tests. `TANGO_TEST_POSTGRES_DSN` is gone, with no fallback.

In a Postgres run each `testdb.Open` creates a schema of its own, sets it as every connection's `search_path`, and drops it with `CASCADE` when the test ends. Tests can commit, run DDL and run in parallel without seeing each other. An unreachable server or a malformed DSN fails the test, so a Postgres run can't pass by skipping. A test that genuinely can't run on one dialect says so with `SQLiteOnly` or `PostgresOnly` and a reason, which keeps every exemption greppable. A SQLite file path in the DSN only selects file-backed mode and is never opened, because a file shared between tests would leak rows from one into the next.

Rejected:

- **Running only the five gated files on Postgres.** It misses SQL generated everywhere else: the point is to catch Postgres behavior SQLite tolerates, wherever it comes from.
- **Looping each test over both dialects.** It doubles every test's output, and it rules out running the dialects as parallel CI legs.
- **Two variables, a dialect plus a DSN.** They can disagree. The scheme already says which dialect a DSN is for, and runtime config follows the same rule.
- **Wrapping each test in a rolled-back transaction.** It breaks tests that commit, run DDL, or use concurrent connections, and the migration runner's non-transactional foreign-key preflight ([ADR 0012](0012-fk-referential-integrity-check-is-a-non-transactional-preflight.md)).
- **A database per test.** Creating databases is much slower than creating schemas, and it needs privileges a test role often lacks.

The cost: `testdb` imports both the SQLite and pgx drivers, so a test binary that uses it links both. Only test binaries do. A Postgres run also needs a role allowed to create schemas in the target database. Tests that inspect SQLite internals (`sqlite_master`, `PRAGMA`) stay SQLite-only until they're rewritten against both dialects.
