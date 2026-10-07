# bubbletea/bubbles/lipgloss for `tango tui`, no `huh`

## Context

`tango tui` is an optional terminal UI, built as a thin layer over existing, already-scriptable CLI commands: a read-only status dashboard (app/check/database/migration state) plus a small menu of actions (run server, apply/roll back migrations, open shell when available). This needs a real choice of terminal UI library, and that choice is hard to change later once screens, key bindings, and styling are built against a specific framework's model.

Alternatives considered:

- **`bubbletea` + `bubbles` + `lipgloss`** (Charm's stack): an Elm-architecture TUI runtime, a library of common widgets (lists, viewports, spinners), and a terminal styling library. Widely used in the Go ecosystem for exactly this kind of "developer console" tool.
- **`tview`**: a more traditional immediate-widget-tree TUI library; less idiomatic Go (heavier on mutable widget state), smaller ecosystem of reusable pieces for this specific shape of screen.
- **Hand-rolled ANSI/`termbox-go`**: full control, but reinvents layout, input handling, and styling primitives that `bubbletea`'s stack already provides.
- **`huh`** (Charm's guided-forms library, layered on `bubbletea`): considered specifically for the migration confirmation prompt.

## Decision

Use `bubbletea` as the application runtime, `bubbles` for widgets, and `lipgloss` for styling — the stack the TUI was planned around. Do not add `huh`: the only form-like interaction the TUI needs is a single yes/no confirmation before a destructive action (apply/roll back migrations), which is built directly with `bubbletea`/`bubbles`/`lipgloss` rather than pulling in a whole forms library for one prompt.

The TUI package lives under the CLI command tree (not the framework core), so applications importing `github.com/angvp/tango` never pay for these dependencies.

## Consequences

- Adding `huh` later (e.g. for a genuinely multi-field guided flow, such as an interactive `newproject`/`newapp` wizard) is a low-cost additive change, since `huh` is itself built on `bubbletea`.
- The TUI's dependency footprint stays in `internal/cli` (or a new internal TUI subpackage), never in `go.mod`'s root-package-reachable dependency graph.
- Switching TUI foundations later (e.g. to `tview`) would mean rewriting every screen — this is the real cost this ADR accepts in exchange for a well-supported, idiomatic-Go stack today.
