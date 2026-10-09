# Agent Recipe: `tango shell`

Use this when you need to look at or change a project's data, check what a model or query returns, call a project helper, or when changing or documenting the shell itself.

Canonical files: `shell/shell.go`, `internal/shellcore/run.go`, `internal/shellcore/verbs.go`, `internal/shellcore/limits.go` and `internal/cli/shell.go`. Human guide: `docs/guides/shell.md`.

## Pick the tool

- **The shell** for a quick look or a one-off fix on a development database: counting rows, reading one, correcting a field, trying a query shape, calling a project helper. It is a local tool that runs with the project's credentials, so use it on a database you may change, and add `--readonly` when you only need to read.
- **A test** (`testdb`, see `docs/agents/testing.md`) when the answer should stay true: it is repeatable and checked in CI.
- **A temporary `main` package** only when you need Go the interpreter cannot run (the `min`, `max` and `clear` builtins, generics it cannot infer, ranging over a function or an integer) or your own types with their methods.

## Use it without a terminal

`tango shell -c '<expression>'` runs lines, prints results to standard output and errors to standard error, and stops at the first error with a non-zero status. Models are `app.Model`, rows are `map[string]any`, and a query is a map with `where` (equality), `order`, `limit` and `offset`:

```text
tango> Count("posts.Post")
2
tango> List("posts.Post", map[string]any{"where": map[string]any{"Title": "Hello"}, "order": []string{"-ID"}, "limit": 10})
map[Body:"From the shell" ID:1 Title:"Hello"]
tango> Update("posts.Post", 2, map[string]any{"Title": "Draft, revised"})
map[Body:"Not ready" ID:2 Title:"Draft, revised"]
```

Run `Models()` first to see the names, and `Describe("app.Model")` for a model's fields.

## Rule

- `tango shell` builds the project's `shell/main.go` and runs it with the arguments. The `tango` command and the server never link the interpreter; only `shell/main.go` does, through `github.com/angvp/tango/shell`. If a project has no `shell/main.go`, the command says which file is missing; the "Adding the shell to an existing project" section of `docs/guides/shell.md` has the upgrade.
- `shell.Run(ctx, config, store, args, opts)` boots the registry (`BuildRegistry`, `RunRegistration`, `SetStore`) and starts nothing else. Its exported surface is `Run` and `Options{ReadOnly, Helpers, DatabaseLabel}` only; everything else lives in `internal/shellcore`.
- A project's apps are listed once, in `project/project.go`, which `main.go` and `shell/main.go` both call. Do not copy an app list into the shell.
- `DatabaseLabel` must never carry a credential. The scaffold derives it from the parsed DSN and hides anything it cannot read with confidence; keep that rule if you touch it.
- What the interpreter cannot run is one table, `Limitations` in `internal/shellcore/limits.go`. The hint, `help()`, `--help`, the guide and a regression test all read from it. Add a row there and in `docs/guides/shell.md` together.
- The interpreter keeps only the first value of a call to a function it does not know, so the shell reads every result of its own helpers, project helpers and standard library calls; for anything else, assign the results.

## Test it

- Helper behaviour: `internal/shellcore/verbs_test.go`, on SQLite and, with `TANGO_TEST_DSN`, PostgreSQL.
- The real thing: `internal/cli/newproject_shell_test.go` scaffolds a project, runs `tango shell -c` and piped input against it and checks the database label against DSNs that carry passwords; `internal/cli/shell_pty_unix_test.go` drives it on a pseudo-terminal (prompt, Ctrl-C, Ctrl-D, history, exit status `130`); `internal/cli/newproject_upgrade_test.go` upgrades a pre-shell project.
- The guide, the tutorial and this recipe quote one session: `internal/cli/testdata/shell_session.txt`. `TestShellDocsQuoteTheTestedSession` fails when a quoted line or its output stops matching. Change the script and the docs together.
- A Yaegi upgrade needs a changelog entry, a rerun of `TestEveryDocumentedLimitationStillFails` and the scaffold tests, and a docs change if what runs has changed.

## Don't

- Do not promise the Go subset, the formatting beyond sorted keys and one row per line, or the wording of errors and hints: they are best-effort (`docs/compatibility.md`).
- Do not add raw SQL, an app list, or a way to serve HTTP to the shell.
- Do not import `internal/cli` or `internal/shellcore` from a project.
