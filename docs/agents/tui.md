# Agent Recipe: `tango tui`

Use this when changing or documenting the `tango tui` dashboard, or advising a user who runs it.

Canonical files: `internal/cli/tui.go`, `internal/cli/tui_dashboard.go`, and the tests `internal/cli/tui_eventloop_test.go` and `internal/cli/tui_guide_test.go`. Human guide: `docs/guides/tui.md`.

## Rule

- `tango tui` is a client of the project's own binary: it runs `go run . -tango-status` for status and `go run .`, `go run . -migrate` or `go run . -migrate -down` for actions. It never opens the database itself.
- Which actions can run comes from one function, `availability`, over `tango.ProjectStatus`. Add no query or state just for the dashboard; if the dashboard needs a fact the status lacks, add it to `ProjectStatus` first (a Covered JSON shape, so additive only).
- A failed apply or rollback stays in the dashboard with a `Last action failed: …` line; only "Run server" ends it, and a stop by Ctrl-C or `SIGTERM` is a normal exit (`server stopped`, code 0).
- Without a terminal it prints status and exits `0`. Tests supply the terminal answer through the `interactive` parameter of `tui`.

## Test it

- Drive the real event loop with scripted keys through Bubble Tea's `WithInput` and `WithOutput`, one key per read (Bubble Tea merges characters from one read into a single key). Use `scriptedSessions` and `session` in `tui_eventloop_test.go`; do not add `teatest`.
- The guide's screens are the expected values: `TestTUIGuideShowsWhatTheDashboardPrints` fails when the screen and `docs/guides/tui.md` disagree. Change both together.

## Don't

- Do not promise layout, wording or key bindings in docs: only the command's existence, the terminal requirement, confirmation before database changes and delegation to the supported operations are Covered (`docs/compatibility.md`).
- Do not import `internal/cli` from a project; the TUI dependencies stay out of the root library's import graph.
