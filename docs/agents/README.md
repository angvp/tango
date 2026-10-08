# tanGO agent context pack

Read `AGENTS.md` first. Then choose only the recipes needed for the task; this keeps local context small while preserving tanGO's non-negotiable conventions.

## New application or local feature

Read in this order:

1. `project-shape.md` — choose Small, Medium, or Hexagonal only when earned.
2. `routing.md` — connect Views to named URLs.
3. `models.md` and `migrations.md` — only when persistent data is needed.
4. `admin-registration.md` — only when the model belongs in admin.
5. `checklist.md` — before declaring work complete.

## Add only the capability needed

- Authentication: `auth-and-accounts.md`; use `jwt-auth.md` for stateless API/WebSocket tokens.
- Outgoing email: `mail.md`.
- Relationships: `relationships.md`.
- Reusable package: `reusable-apps.md`.
- Request cross-cutting concern: `middleware.md`; use `ratelimit.md` for quotas and `observability.md` for logs/metrics.
- Locale-aware text: `i18n.md`.
- Live rooms/WebSockets: `realtime.md`; pair with `application-lifecycle.md` for graceful shutdown.
- Recurring work: `jobs.md`; pair with `application-lifecycle.md` because Jobs run only through `ServeContext`.

## Escalate deliberately

Use the referenced human guide or runnable example when a recipe says the surface is intentionally limited, the task combines multiple capabilities, or the app needs behavior outside the documented public API. Never solve that by importing `internal/` packages or copying framework internals.
