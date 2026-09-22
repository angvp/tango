# tanGO Agent Notes

This file is best-effort guidance for coding agents working in tanGO projects. It is not a versioned or stable contract; tanGO is still a v0.0.1 framework and its public API is still settling.

Use this as the universal contract, then use `docs/agents/` for task-specific recipes.

## Always Do

- Treat plain Go structs as the source of truth for models and metadata.
- Register apps explicitly through `tango.Config{InstalledApps: []tango.App{...}}`.
- Register models, admin config, routes, checks, and app-owned assets from an app's `Register(*tango.Registry)` path.
- Use public packages and documented APIs first: `tango`, `model`, `db`, `auth`, `accounts`, `admin`, `i18n`, and `migration` through the documented workflow.
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

- Project/app shape: `docs/agents/project-shape.md`
- Models: `docs/agents/models.md`
- Migrations: `docs/agents/migrations.md`
- Admin registration: `docs/agents/admin-registration.md`
- Reusable apps: `docs/agents/reusable-apps.md`
- Relationships and foreign keys: `docs/agents/relationships.md`
- Auth and accounts: `docs/agents/auth-and-accounts.md`
- JWT authentication: `docs/agents/jwt-auth.md`
- Realtime rooms and WebSockets: `docs/agents/realtime.md`
- Rate limiting: `docs/agents/ratelimit.md`
- Middleware and View wrappers: `docs/agents/middleware.md`
- Optional prompt snippets: `docs/agents/prompts.md`

## Sync Rule

When a public API or canonical example changes, review affected files in `docs/agents/`. These docs intentionally prefer references over duplicated code, so stale references are caught by ordinary doc review rather than tooling.
