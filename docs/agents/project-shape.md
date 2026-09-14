# Agent Recipe: Project and App Shape

Use this when creating a new tanGO project or adding a local app to an existing project.

Canonical example: `examples/api-with-admin/`.

## Decide First

- Build a local project app when the code belongs only to this host project.
- Build a reusable app when the package is meant to be imported by another project; use `docs/agents/reusable-apps.md` for that.
- Pick a shape. Default to Small. Only escalate when a concrete signal is present — see "Choosing a shape" below.

## Choosing a shape

Default to Small (`models.go`/`views.go`/`urls.go`/`admin.go` at app root) unless a concrete signal is already present: escalate to Medium (adds `services/` and `repositories/`-or-`store/`; no `ports/` yet) once views hold application decisions — validation spanning more than one field, multi-step writes, logic duplicated across views — and escalate to Hexagonal (adds `domain/`, `ports/`, `adapters/{db,http,admin}/`, the only tier where persistence structs move under `adapters/db/` separate from `domain/` types and admin registration moves into `adapters/admin/`) once the app has real business rules, multiple interfaces, reusable domain logic, or long-term maintenance pressure; never escalate "for future-proofing" with no signal present — an unused `ports/` interface with one implementation is ceremony, not architecture. Full rationale and code snippets: [application architecture guide](../guides/application-architecture.md) — this repo ships no runnable Hexagonal example under `examples/`, so treat that guide's snippets as illustrative, not a canonical tested reference.

## Do

- Put local app code under a normal Go package inside the host module, for example `apps/posts/`.
- Give each app an explicit constructor or value returning `tango.App`.
- Register the app from `main.go` through `tango.Config.InstalledApps`.
- Keep DB setup in the host project and pass shared dependencies, such as `*db.Store`, into apps that need them.

Tiny shape:

```text
main.go
apps/<name>/app.go
apps/<name>/models.go
apps/<name>/views.go
migrations/
```

## Don't

- Do not discover apps by walking directories.
- Do not register models/routes from `init`.
- Do not expose Chi or other router internals from app packages.

## Check

- Compare project layout with `examples/api-with-admin/main.go` and `examples/api-with-admin/apps/posts/app.go`.
- Run the shared checklist: `docs/agents/checklist.md`.
