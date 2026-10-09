# tanGO Agent Notes

This file is best-effort guidance for coding agents working in tanGO projects. It is not a versioned or stable contract; tanGO is still early and its public API is still settling.

Start with this file, then read `docs/agents/README.md` to choose the smallest task-specific recipe. The recipes are the compact local context pack; consult guides, examples, and source only when the recipe explicitly points there or the task needs a capability outside it.

## Always Do

- Treat plain Go structs as the source of truth for models and metadata.
- Register apps explicitly through `tango.Config{InstalledApps: []tango.App{...}}`.
- Register models, admin config, routes, checks, and app-owned assets from an app's `Register(*tango.Registry)` path.
- Use public packages and documented APIs first: `tango`, `model`, `db`, `auth`, `auth/jwt`, `accounts`, `admin`, `i18n`, `mail`, `mail/mailtest`, `ratelimit`, `realtime`, `realtime/websocket`, `observability`, and `migration` through the documented workflow.
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

- Start-here reading order: `docs/agents/README.md`
- Project/app shape: `docs/agents/project-shape.md`
- Models: `docs/agents/models.md`
- Migrations: `docs/agents/migrations.md`
- Routing and reverse lookup: `docs/agents/routing.md`
- Admin registration: `docs/agents/admin-registration.md`
- Reusable apps: `docs/agents/reusable-apps.md`
- Relationships and foreign keys: `docs/agents/relationships.md`
- Auth and accounts: `docs/agents/auth-and-accounts.md`
- Outgoing email: `docs/agents/mail.md`
- JWT authentication: `docs/agents/jwt-auth.md`
- Realtime rooms and WebSockets: `docs/agents/realtime.md`
- Background jobs: `docs/agents/jobs.md`
- Application lifecycle and graceful shutdown: `docs/agents/application-lifecycle.md`
- Configuration and deployment: `docs/agents/configuration-and-deployment.md`
- Testing with `testdb`: `docs/agents/testing.md`
- The `tango tui` dashboard: `docs/agents/tui.md`
- Running more than one instance: `docs/agents/running-more-than-one-instance.md`
- Rate limiting: `docs/agents/ratelimit.md`
- Localization: `docs/agents/i18n.md`
- Middleware and View wrappers: `docs/agents/middleware.md`
- Structured logging and metrics: `docs/agents/observability.md`
- Optional prompt snippets: `docs/agents/prompts.md`

## Sync Rule

When a public API or canonical example changes, review affected files in `docs/agents/`. Each public feature must either have an agent recipe, extend an existing recipe, or explicitly be documented as too narrow to need one. These docs intentionally prefer references over duplicated code. A test (`agent_recipes_test.go`) fails when a recipe names a file, example or docs page that doesn't exist, or links no human guide; whether the prose still describes the code is caught by review.
