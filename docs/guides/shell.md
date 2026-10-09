# Guide: `tango shell`

`tango shell` opens a Go prompt on your project: its configuration, database, installed apps and model registry are loaded, and nothing is served. Type an expression, see the result, and the variables you set are still there on the next line. It replaces the throwaway Go program you would otherwise write to look at, or fix, a few rows.

**It runs your code with your project's database credentials.** It is a developer tool for your own machine, not a sandbox and not a way to administer a server remotely: anything you can type runs with the same access as your application.

## Start it

From the project directory:

```sh
tango shell
```

`tango shell` builds the project's own `shell/main.go` and runs it. That program lives in your module because only there are your models linked; `tango newproject` writes it, and the `tango` command itself never links the interpreter, nor does your server. (A project made before the shell existed adds the file by hand; see [adding the shell to an existing project](#adding-the-shell-to-an-existing-project).) The shell loads `.env` and opens the database named by `TANGO_DB_DSN` the way the server does, registers your apps, and starts nothing else: no HTTP server, no jobs, no lifecycles. It prints which database it is on, with every credential left out, so you can see where you are before you change anything.

## A first session

Models are addressed as `app.Model`, and a row is a `map[string]any` keyed by field name. This is a session on a fresh database with one `posts.Post` model (`ID`, `Title`, `Body`) beside the admin's own models:

```text
tango> Models()
["admin.AdminSession" "admin.AdminUser" "posts.Post"]
tango> Create("posts.Post", map[string]any{"Title": "Hello", "Body": "From the shell"})
map[Body:"From the shell" ID:1 Title:"Hello"]
tango> Create("posts.Post", map[string]any{"Title": "Draft", "Body": "Not ready"})
map[Body:"Not ready" ID:2 Title:"Draft"]
tango> Count("posts.Post")
2
tango> List("posts.Post", map[string]any{"where": map[string]any{"Title": "Hello"}, "order": []string{"-ID"}, "limit": 10})
map[Body:"From the shell" ID:1 Title:"Hello"]
tango> Update("posts.Post", 2, map[string]any{"Title": "Draft, revised"})
map[Body:"Not ready" ID:2 Title:"Draft, revised"]
tango> rows, err := List("posts.Post")
tango> len(rows)
2
tango> Delete("posts.Post", 2)
tango> Count("posts.Post")
1
```

`Models()` lists what is registered, including the admin's tables. `rows, err := List(...)` followed by `len(rows)` shows that it is ordinary Go: variables stay around from one line to the next. A call that returns an error shows it, and a non-interactive run stops there.

## The helpers

| Helper | What it does |
|---|---|
| `Models()` | Every registered model, as `app.Model`, sorted. |
| `Describe("app.Model")` | One row per field: `Name`, `Type`, `PrimaryKey`, `Unique`, `Indexed` and `ForeignKey` (the target as `app.Model`, or empty). |
| `Get("app.Model", pk)` | The row with that primary key; an error if there is none. |
| `List("app.Model", query)` | The rows matching an optional [query](#queries). |
| `Count("app.Model", query)` | How many rows match the query's `where`. |
| `Create("app.Model", row)` | Stores a row built from a field-to-value map and returns it with the primary key the database assigned. |
| `Update("app.Model", pk, changes)` | Changes the given fields of that row and returns the stored row. The primary key cannot be changed. |
| `Delete("app.Model", pk)` | Removes the row. |
| `Context()` | The session's `context.Context`, for calling your own code. |

A model name must be fully qualified. `List("Post")` is an error that suggests `posts.Post`, because two apps may one day have a model of the same name.

Row values keep their Go types: an `int64` ID, a `time.Time`, a `bool`. Values you pass in are converted when that loses nothing (an `int` for an `int64` field), and refused when it would (`"1"` for an integer, or `1.5` for an `int`). The foreign-key checks, cascading deletes and other rules of `db.Store` apply exactly as they do in your application, because the helpers call it.

There is no raw SQL helper.

### Queries

`List` and `Count` take one optional `map[string]any`:

| Key | Meaning |
|---|---|
| `"where"` | A `map[string]any` of field name to value. Every pair must match (equality only). |
| `"order"` | A `[]string` of field names; a leading `-` means descending, as in `"-ID"`. |
| `"limit"` | At most this many rows. |
| `"offset"` | Skip this many rows first. |

`Count` looks only at `"where"`. An unknown key, an unknown field or a value of the wrong type is an error that names the problem, never a silently ignored filter.

## Read-only

```sh
tango shell --readonly
```

makes `Create`, `Update` and `Delete` fail with an error that names the flag, while reads work. It covers the built-in helpers only: code you type that opens its own connection, or a helper of your own that writes, is not stopped.

## Scripts and agents

`-c` evaluates one expression, or several lines, prints the results and exits. With standard input from a pipe, each complete statement is evaluated in turn:

```sh
tango shell -c 'Count("posts.Post")'
echo 'Count("posts.Post")' | tango shell
```

Results go to standard output. Errors, panics and the database line go to standard error, so a script can use the output directly. The first error stops the run with a non-zero exit status. The exit codes are:

| Code | Meaning |
|---|---|
| `0` | The session ended cleanly (`exit()`, `quit()`, Ctrl-D, end of input) or `--help` was asked for. |
| `1` | A non-interactive run hit an error, or the project failed to boot. |
| `2` | The command line was not understood. |
| `130` | Ctrl-C interrupted a running evaluation. |

Values print deterministically: map keys are sorted, times are RFC 3339, a `[]byte` shows its length and a short prefix, and a list of rows prints one row per line. Declarations, assignments and `nil` print nothing.

## At the prompt

On a terminal you get a prompt with line editing and the up arrow.

- A statement continues on the next line while a brace, bracket or parenthesis is open, and the prompt changes to show it.
- `exit()`, `quit()` and Ctrl-D leave.
- **Ctrl-C** at the prompt clears the line and says how to leave; a second Ctrl-C in a row leaves. Ctrl-C **during** an evaluation ends the process with status `130`: the interpreter cannot interrupt a running evaluation, so a runaway loop is ended by ending the shell. Changes that were already made stay made; the shell does not wrap a session in a transaction.
- An error or a recovered panic is printed as `error: …` or `panic: …` and the session carries on. A failure the shell recognises as a limit of the interpreter gets one extra `hint:` line.
- A single printed value is cut at 200 characters with a visible `…` at a terminal. With `-c` or a pipe nothing is cut.
- `help()` prints the helpers and the list of what the interpreter cannot run; `tango shell --help` prints the options and the same list.

### History

Up to 1000 lines are kept per project in your user cache directory (for example `~/.cache/tango/shell-history` on Linux), in a file only you can read, and the next session in that project starts with them. **Anything you type is kept, including a password or a token you paste into an expression.** Set `TANGO_SHELL_HISTORY=off` to keep nothing on disk; the up arrow still works within the session.

## Your own helpers

Code of your own that you want in the console (a backfill, a recompute, a test email) goes in the `Helpers` option of your `shell/main.go`:

```go
		Helpers: map[string]any{
			// Call your own application code from here. In the shell these are
			// project.Greeting("...") and project.Fail().
			"Greeting": func(name string) string { return "hello, " + name },
			"Fail":     func() error { return errors.New("reindex failed") },
		},
```

Inside the shell they are reached only as `project.Name`, so they cannot shadow a built-in helper:

```text
tango> project.Greeting("shell")
"hello, shell"
tango> project.Fail()
error: reindex failed
```

A helper closes over whatever it needs (the store, your services, a context): the shell passes nothing to it. A name must be an exported Go identifier, and the shell refuses to start otherwise. `help()` lists the names.

Only plain values are supported across this boundary: numbers, strings, booleans, maps, slices and `error`. A value of one of your own types arrives, but using its fields and methods from the prompt is not something the shell promises.

## What the interpreter cannot run

The shell interprets Go with [Yaegi](https://github.com/traefik/yaegi), pinned at v0.16.1, which understands Go 1.21 and 1.22. Your project is on a newer Go, so valid current Go can fail at the prompt. `tango shell` does not pretend otherwise: it prints the failure, one hint pointing here, and keeps the session.

| Does not work | Write this instead |
|---|---|
| The min and max builtins | compare with if, or write a small function |
| The clear builtin | delete the keys in a loop: for k := range m { delete(m, k) } |
| Inferring the type arguments of a generic function whose result type differs from its argument types | give the type arguments explicitly, as in Map[int, string](xs, f) |
| Ranging over a function, or over an integer (for i := range 3) | use a counting for loop, or call the function with a callback |

Two more things to know:

- A call to *your own* function that returns several values shows only the first. Assign them (`n, err := f()`) to see the rest. The built-in helpers and the standard library's functions show every value, and an error among them.
- A panic Yaegi raises on Go it cannot run (as with ranging over a function) is recovered and reported; it does not end the session.

When a release of the interpreter supports something above, the test that lists these fails until this table and the shell's own list change together.

## Adding the shell to an existing project

A project made with `tango newproject` before the shell existed lists its apps in `main.go`. The shell needs them in a place both `main.go` and `shell/main.go` can reach, so the upgrade is one mechanical move:

1. Create `project/project.go` with `func Config(store *db.Store) tango.Config`, and move into it everything `main.go` does to build `config`: the `tango.LoadConfigFromEnv(...)` call, `InstalledApps`, `Middleware` and `MiddlewareScope`. End it with `return config`. It must not load `.env` or open a database.
2. In `main.go`, build the store (`store := db.NewStore(sqlDB, dsn.Dialect)`), replace the lines you moved with `config := project.Config(store)`, and import the new package.
3. Create `shell/main.go`.

The quickest way to get files 1 and 3 right is to scaffold a scratch project in an empty directory (`tango newproject scratch`) and copy `scratch/project/project.go` (then paste in your own app list) and `scratch/shell/main.go` into your project, changing the module name in the imports. `shell/main.go` loads `.env`, opens the database like `main.go`, calls `project.Config(store)` and `shell.Run`; for a PostgreSQL project, keep the driver import your `main.go` has.

For a project from the plain `tango newproject legacyapp` scaffold, this is the change to `main.go`; the new files are `project/project.go`, holding the lines removed here, and `shell/main.go`:

```diff
diff --git a/main.go b/main.go
index 8e79dd6..b31def2 100644
--- a/main.go
+++ b/main.go
@@ -14,6 +14,7 @@ import (
 	"github.com/angvp/tango/db"
 
 	"legacyapp/migrations"
+	"legacyapp/project"
 
 	_ "modernc.org/sqlite"
 )
@@ -42,24 +43,9 @@ func run() error {
 	defer sqlDB.Close()
 
 	store := db.NewStore(sqlDB, dsn.Dialect)
-	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
-	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
-	config.InstalledApps = []tango.App{
-		admin.New(store),
-	}
-	config.Middleware = []tango.Middleware{
-		tango.RequestID(),
-		tango.Recoverer(),
-		tango.AccessLogger(),
-		// Request bodies are capped at 1 MiB. If this application later needs
-		// large uploads, remove the global body-limit middleware and apply
-		// `MaxBodySize` only to the route groups or routes that should remain
-		// limited.
-		tango.MaxBodySize(1 << 20),
-	}
-	// Global middleware also wraps requests no route matches, so 404s and
-	// 405s are logged and counted too.
-	config.MiddlewareScope = tango.MiddlewareScopeAll
+	// The installed apps, middleware and other configuration live in
+	// project/project.go, shared with the shell.
+	config := project.Config(store)
 
 	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
 		return err
```

The upgrade is tested against the scaffold as it was before the shell: [`internal/cli/testdata/legacy_scaffold`](../../internal/cli/testdata/legacy_scaffold) holds that scaffold and the full diff for both variants, and a test applies them and runs the result. A project that does not upgrade keeps running as a server; only `tango shell` needs the new files, and without them it says which file is missing and links here. Once `project/project.go` exists, `tango newapp` tells you to add new apps there.

## What is covered

The command, its options and exit codes, `shell.Run` and `shell.Options`, the helpers above with their arguments and the `project.` qualifier, `help()`, `exit()`, `quit()` and `TANGO_SHELL_HISTORY=off` are part of the Covered API. The interpreter, the exact Go it can run, the formatting beyond what this page states, the wording of errors and hints, the history file and the generated `project/` and `shell/` files are not. See [versioning and compatibility](../compatibility.md).
