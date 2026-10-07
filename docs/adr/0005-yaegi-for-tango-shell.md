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
