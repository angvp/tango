# tanGO Agent Notes

This file is best-effort guidance for coding agents working in tanGO projects. It is not a versioned or stable contract; tanGO is still early and its public API is still settling.

Start with this file, then read `docs/agents/README.md` to choose the smallest task-specific recipe. The recipes are the compact local context pack; consult guides, examples, and source only when the recipe explicitly points there or the task needs a capability outside it.

## Always Do

- Treat plain Go structs as the source of truth for models and metadata.
- Register apps explicitly through `tango.Config{InstalledApps: []tango.App{...}}`.
- Register models, admin config, routes, checks, and app-owned assets from an app's `Register(*tango.Registry)` path.
- Use public packages and documented APIs first: `tango`, `model`, `db`, `auth`, `auth/jwt`, `accounts`, `admin`, `i18n`, `mail`, `mail/mailtest`, `ratelimit`, `realtime`, `realtime/websocket`, `observability`, `openapi`, `openapi/swaggerui`, `cache`, `cache/local`, `cache/redis`, `cache/memcache`, `storage`, `storage/local`, `storage/s3`, `testdb` (tests only), `shell` (only for `tango shell`), and `migration` through the documented workflow. `storage/s3`, `cache/redis` and `cache/memcache` are separate Go modules with their own `go.mod`: add one to a project only when it needs that backend.
- Use `db.Store` for persistence unless a real query need forces raw SQL through `Store.Query`/`QueryRow`.
- Keep Chi as tanGO's internal routing implementation detail. Do not expose Chi types from app APIs.
- Run the shared validation checklist in `docs/agents/checklist.md` before calling a change done.

## Never Do

- Do not scan the filesystem for apps, models, routes, migrations, or admin registration.
- Do not use global `init` registration.
- Do not import packages under `internal/` from a project or reusable app.
- Do not generate YAML/JSON sidecar schemas for models; write Go structs.
- Do not duplicate large blocks from examples into docs or prompts; reference canonical examples instead.

## Recipes

The recipe index is `docs/agents/README.md`: choose the smallest task-specific recipe there. It lists every recipe, including storage and cache, and is the only index.

## Sync Rule

When a public API or canonical example changes, review affected files in `docs/agents/`:

- A new public package goes in the import line above, and a new recipe is linked from `docs/agents/README.md`. `agent_index_test.go` fails until both are done.
- A behavior change that makes a recipe's directive false fixes that directive in the same change.
- Each public feature either has an agent recipe, extends an existing one, or is too narrow to need one (for example a presentation-only change); say which in the commit message.
- Recipes prefer references over duplicated code. `agent_recipes_test.go` also fails when a recipe names a file, example or docs page that doesn't exist, or links no human guide. Whether the prose still describes the code is caught by review.
