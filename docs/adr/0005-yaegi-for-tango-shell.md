# Yaegi as the direction for `tango shell`

## Context

Go has no native REPL. Django's `manage.py shell` (drop into a Python REPL with models pre-imported) is the experience `tango shell` aims to approximate, but Go's compiled, statically-typed nature makes a literal port impossible. Three realistic directions:

1. **Yaegi** (`github.com/traefik/yaegi`): a pure-Go interpreter that can `Eval` arbitrary Go source at runtime, import compiled packages via reflection, and run as a normal library inside a normal binary. Gives an actual "type an expression, see a result" REPL loop.
2. **Generated helper binaries**: each shell command is written to a throwaway `main.go`, compiled, and run via `go run`. Works with real Go semantics (no interpreter gaps) but pays a compile step per command — too slow for an interactive loop, and doesn't hold state between commands without extra machinery (serializing/rebuilding a session).
3. **Delve** (`github.com/go-delve/delve`): a real debugger that attaches to a running process via ptrace, for stepping through and inspecting a live program. It solves "pause and inspect," not "evaluate an arbitrary new expression against my app's registered models" — a different problem shape entirely, and heavier (ptrace, DWARF parsing) for something that isn't debugging.

## Decision

`tango shell`'s direction is Yaegi. It's the only option of the three that supports an actual read-eval-print loop — evaluate an expression, see a result, keep going, with state persisting across the session — without a subprocess per command or a debugger's process-attachment model. Apps' registered models/registry would be exposed to the interpreter's symbol table so shell sessions can query/construct them directly.

This is a direction decision only. `tango shell` is not implemented yet.

## Consequences

- Yaegi doesn't implement 100% of Go (some reflection-heavy or generics-heavy code, and anything relying on unsafe/cgo, can hit interpreter gaps). `tango shell` sessions may occasionally hit expressions Yaegi can't evaluate that `go run` would handle fine — an accepted limitation of the trade-off, not a blocker to choosing it.
- No subprocess/compile step per shell command — the interpreter lives inside the running shell binary, so state (variables, imports) persists naturally across a session.
- Exposing registered models/registry to Yaegi's symbol table needs a defined bridge (likely reflection-based, similar to how `Store` already does reflection over `ModelMeta`) — that bridge design is deferred until `tango shell` is implemented.
- Switching away from Yaegi later (e.g. to a from-scratch interpreter) is possible but would mean redesigning the shell's evaluation loop — this is a real, if distant, cost of committing now rather than re-deciding at implementation time.

## Update (2026-10-09): a spike found the direction workable, with limits

A bounded spike checked whether Yaegi works on the Go and tanGO this project is on now. It built throwaway code outside the repository and shipped none. It used Go 1.27.1, Yaegi v0.16.1 (the latest release, April 2024, which promises support for Go 1.21 and 1.22) and Yaegi master as of February 2026.

**What worked, on both Yaegi versions.**

- Yaegi builds and runs under Go 1.27 and evaluates expressions, and a variable set on one line is there on the next.
- From the interpreter, `Store` calls against SQLite work, with `yaegi extract` symbol tables for tanGO's `db` and `model` packages (the exported `Store` API is reflection-based, not generic).
- Against a scaffolded project, both approaches created and listed a row. **Typed:** a symbol table generated from the project's own package lets the interpreter use its model types. **Maps:** a shell binary exposes a few helper functions over the registry and `Store`, and the interpreter sees only maps, with nothing generated.

**What did not work.** Yaegi interprets an older Go than the one the shell would run on. Expressions a user would reasonably type fail:

- the `min`, `max` and `clear` builtins are undefined;
- type inference fails for a generic function whose result type differs from its element type (a `Map[T, U]` called with a function from `int` to `string` gives `cannot use type func(int) string as type func(int) int`); a generic function with one type parameter worked;
- a range over a function panics, and on v0.16.1 so does a range over an integer; the panic comes out of `Eval`, so the shell must recover it or one line of valid Go ends the session.

**What a project has to carry.** Both approaches ran as a shell program inside the project's own module, because only there are its model types linked. A `tango` CLI process does not link them, so a `tango shell` command would have to run the project's own binary, as `tango tui` does. It repeats the project's wiring (DSN, driver import, store, installed apps, `BuildRegistry` and `RunRegistration`). The typed approach also needs a generated symbol package that must be regenerated whenever a model changes, or it silently describes the old struct. The shell binary was 35.6 MB beside the project's 20.8 MB. All of it stays out of the root library's import graph, as with the TUI ([ADR 0007](0007-bubbletea-stack-for-tango-tui.md)).

**Decision.** The direction stands: Yaegi remains the only option that gives a real read-eval-print loop, and the spike shows a prototype can work, not that a supported shell exists. `tango shell` is still not implemented, and 0.3.0 ships no `tango shell` command, hidden or otherwise. Building it is a separate milestone that has to decide:

1. How the shell reaches the project: a shell program the scaffold generates, or a flag the project's `main.go` handles, as `-tango-status` is.
2. Typed access with generated, regenerated symbol tables, or map rows with helper functions tanGO ships and keeps stable.
3. Which Yaegi version to depend on, given a release that predates Go 1.27 and a master that fixes only some of the gaps above, and what to tell users about the Go it cannot interpret.
4. How panics and unsupported expressions are reported without ending the session.

**Built.** [ADR 0048](0048-tango-shell-is-the-projects-own-program-on-a-pinned-yaegi.md) records how the shell was built on this direction.
