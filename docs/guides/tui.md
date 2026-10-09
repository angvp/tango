# Guide: `tango tui`

`tango tui` is a small terminal dashboard for the project in the current directory. It shows whether the project's registration and database are healthy and how many migrations are applied, and lets you apply migrations or roll one back without leaving the screen, or start the server from it.

It changes nothing on its own: every action is the same command you could type yourself, and the two that change the database ask first.

## Start it

From the project directory:

```sh
tango tui
```

It asks the project for its status by running `go run . -tango-status`, so the project's `main.go` must call `tango.DispatchFlags` (every project from `tango newproject` does). That status check is read-only: it does not create `tango_migrations` or touch the database schema.

## The screen

```text
tanGO project status
  registration: ok
  database: reachable
  migrations: 3 total, 2 applied, 1 pending

> Run server
  Apply pending migrations
  Roll back the latest migration

↑/↓ move · enter select · q quit
```

- **registration** is the result of `tango check`'s registration step: your apps, routes and models loaded without error.
- **database** says whether the project could reach its database.
- **migrations** counts the migrations your project defines, how many are recorded as applied, and how many are pending.

Move with the arrow keys (or `j`/`k`), press Enter to choose, and `q` or Ctrl-C to leave.

## The actions

| Action | Runs | Asks first |
|---|---|---|
| Run server | `go run .` | no |
| Apply pending migrations | `go run . -migrate` | yes |
| Roll back the latest migration | `go run . -migrate -down` | yes |

These are exactly what `tango run`, `tango migrate` and `tango migrate down` run, so their behaviour, flags and exit codes are the ones documented for those commands.

### Confirmation

Apply and rollback change your database, so choosing one asks a plain question and waits for `y`. `n` or Esc goes back to the menu and runs nothing; Ctrl-C leaves the dashboard without running it. Other keys are ignored.

```text
tanGO project status
  registration: ok
  database: reachable
  migrations: 3 total, 2 applied, 1 pending

  Run server
> Apply pending migrations
  Roll back the latest migration

Apply pending migrations? (y/n)

↑/↓ move · enter select · q quit
```

The rollback question reads `Roll back the most recently applied migration? (y/n)`. The dashboard does not name the migration: the status it receives carries counts, not names. Run `tango migrate` yourself, or read your `migrations` package, when you need the name.

### When an action cannot run

An action that the status shows cannot do anything is greyed out, with the reason after it, and choosing it does nothing.

```text
tanGO project status
  registration: ok
  database: reachable
  migrations: 3 total, 3 applied, 0 pending

> Run server
  Apply pending migrations (nothing to apply)
  Roll back the latest migration

↑/↓ move · enter select · q quit
```

The reasons are `nothing to apply`, `nothing to roll back`, and `unavailable: project status is incomplete`. The last one appears when registration failed or the database cannot be reached, because the dashboard cannot tell what applying or rolling back would do:

```text
tanGO project status
  registration: FAILED (apps/blog: duplicate route name "post")
  database: reachable
  migrations: 3 total, 3 applied, 0 pending

> Run server
  Apply pending migrations (unavailable: project status is incomplete)
  Roll back the latest migration (unavailable: project status is incomplete)

↑/↓ move · enter select · q quit
```

Fix the problem the status names (here, run `tango check` for the details) and start the dashboard again.

## When an action fails

A failed apply or rollback does not close the dashboard. It shows what failed, refreshes the status where it can, and returns to the menu:

```text
tanGO project status
  registration: ok
  database: reachable
  migrations: 3 total, 2 applied, 1 pending

> Run server
  Apply pending migrations
  Roll back the latest migration

Last action failed: Apply pending migrations (exit code 1): migration 0003_add_title: duplicate column

↑/↓ move · enter select · q quit
```

The line carries the command's exit code and the last line it wrote to standard error, which is where the cause usually is; the full output scrolled past just before the screen came back. If refreshing the status fails too, a second line, `Status refresh also failed: …`, follows the first, and the screen keeps the status from before the action.

`tango migrate` and `tango migrate down` keep their own exit codes; only the dashboard stays open.

## Running the server

"Run server" hands the terminal to your server and ends the dashboard: press Ctrl-C to stop it, as you would with `go run .`. A server stopped that way prints `server stopped` and `tango tui` exits `0`. A server that fails, or exits on its own with an error, keeps its non-zero exit code.

## If the status cannot be loaded

When the project cannot report its status (it does not compile, or its `main.go` predates `-tango-status`), `tango tui` exits `1`. It prints an explanation first, then whatever the project itself wrote to standard error, then the underlying error, unchanged:

```text
tango tui: tanGO could not load project status.
  - Run "tango check" to diagnose registration and configuration.
  - Older project entrypoints may need tango.DispatchFlags, which answers
    -tango-status.
```

One case this cannot catch: a `main.go` that ignores command-line flags entirely starts its server when asked for `-tango-status`, and `tango tui` then waits for a status that never comes. Press Ctrl-C and add `tango.DispatchFlags` to `main.go`; every project from `tango newproject` has it.

## Without a terminal

The interactive dashboard requires a terminal. In non-interactive environments, `tango tui` prints status only; use the dedicated CLI commands to run the server or change migrations.

In a pipe or in CI it prints the same status lines, plus a hint, and exits `0`:

```text
tanGO project status
  registration: ok
  database: reachable
  migrations: 3 total, 2 applied, 1 pending
actions available via: tango run | tango migrate | tango migrate down
```

## What is promised

See [versioning and compatibility](../compatibility.md) for the full policy. For the dashboard, only this is covered:

- `tango tui` exists;
- the interactive dashboard requires a terminal, and without one it prints status only;
- actions that change the database ask for explicit confirmation first;
- it runs the same supported operations as `tango run`, `tango migrate` and `tango migrate down`.

The layout, wording, colours and key bindings are not covered and may change in a minor release. The dashboard screens, the status-failure explanation and the non-interactive output above are checked against the code by a test, so they are accurate for the version these docs describe.

## What the tests cover

The dashboard's logic and its event loop are tested with scripted keystrokes. The one piece no test drives is the check that stdin and stdout are a real terminal (`isInteractiveTerminal`); the tests supply their own answer to it. See [limitations](../limitations.md).
