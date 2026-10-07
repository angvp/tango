# Architecture decision records

Each record explains one decision that is hard to reverse once code depends on it: what was chosen, what was rejected, and what it costs. Records are never deleted. When a later decision changes an earlier one, the earlier record links forward and its status below says so.

New records take the next free number here. This directory is the single, public set of tanGO's decisions.

| # | Decision | Status |
|---|---|---|
| [0001](0001-design-principles.md) | Founding Design Principles | Accepted |
| [0002](0002-chi-as-internal-router.md) | Use Chi as an Internal Router | Accepted |
| [0003](0003-tailwind-play-cdn-for-admin-theme.md) | Use Tailwind's Play CDN for the Admin Default Theme | Accepted |
| [0004](0004-explicit-dialect-for-postgresql-support.md) | Explicit `Dialect` argument for PostgreSQL support | Accepted; env-based loading added later (see note) |
| [0005](0005-yaegi-for-tango-shell.md) | Yaegi as the direction for `tango shell` | Accepted; not implemented yet |
| [0006](0006-typed-go-migration-steps.md) | Migrations as typed Go steps, not raw SQL files | Accepted |
| [0007](0007-bubbletea-stack-for-tango-tui.md) | bubbletea/bubbles/lipgloss for `tango tui`, no `huh` | Accepted |
| [0008](0008-two-helpers-not-one-run.md) | Two small helpers (`DispatchFlags` + `Serve`) instead of one `tango.Run` | Accepted |
| [0009](0009-admin-default-on-in-scaffold.md) | Admin ships by default in `tango newproject`, opt out via `--no-admin` | Accepted |
| [0010](0010-cascade-delete-is-application-level-not-a-db-constraint.md) | Cascade delete happens in `Store.Delete`, not via the database's `ON DELETE CASCADE` | Accepted |
| [0011](0011-fk-target-validated-after-registration-not-at-register-time.md) | A foreign key's target model is validated after every app registers, not when the referencing model registers | Accepted |
| [0012](0012-fk-referential-integrity-check-is-a-non-transactional-preflight.md) | FK referential-integrity check is a non-transactional preflight, and treats zero as unset | Accepted; amended by 0038 |
| [0013](0013-widget-interface-owns-both-render-and-parse.md) | Widget interface owns both render and parse, not render-only | Accepted |
| [0014](0014-admin-extensibility-ships-best-effort-not-stable.md) | Admin extensibility ships best-effort, not as a stable contract | Accepted |
| [0015](0015-context-html-buffers-before-writing.md) | `Context.HTML` buffers a template render before writing, and admin's own renderer is unified onto it | Accepted |
| [0016](0016-quick-create-stays-small-accepts-unsaved-data-loss.md) | Quick-create accepts unsaved-parent-form data loss and ships a narrow, internal preselect convention rather than a general feature | Accepted |
| [0017](0017-app-auth-ships-primitives-not-a-model-or-interface.md) | Application auth ships composable primitives, not a default User model, an interface, or a login view | Accepted; extended by 0020 |
| [0018](0018-middleware-and-view-wrappers-are-two-permanent-layers.md) | Middleware and View wrappers are two permanent, coexisting layers, not one unified mechanism | Accepted |
| [0019](0019-admin-only-boolean-tier-permissions.md) | Permissions ship admin-only, as two boolean tiers, not a per-model matrix or an app-level primitive | Accepted; extended by 0020 |
| [0020](0020-accounts-is-a-conventional-identity-app-not-an-app-permissions-framework.md) | `accounts` is a conventional identity app, not an app-permissions framework | Accepted |
| [0021](0021-accounts-security-defaults-differ-from-admin-where-the-trust-boundary-differs.md) | Public `accounts` uses stricter/different security defaults than admin where the trust boundary differs | Accepted |
| [0022](0022-i18n-is-an-opt-in-override-layer-plain-go-maps-not-a-locale-framework.md) | `i18n` is an opt-in text-override layer built on plain Go maps, not a locale framework | Accepted |
| [0023](0023-agent-facing-docs-are-vendor-neutral-best-effort-and-reference-not-duplicate-canonical-examples.md) | Agent-facing docs are vendor-neutral, best-effort, and reference canonical examples rather than duplicating code or building sync tooling | Accepted; amended (see note) |
| [0024](0024-query-filtering-is-a-bounded-where-primitive.md) | Query filtering ships as a bounded WHERE primitive, not a query DSL | Accepted; amended by 0025 and 0038 |
| [0025](0025-query-filtering-gains-like-any-and-count.md) | Query filtering gains `OpLike`, one OR group, and `Store.Count` | Accepted |
| [0026](0026-jwt-access-tokens-are-stateless-and-non-revocable.md) | JWT access tokens are stateless and non-revocable | Accepted |
| [0027](0027-auth-jwt-wraps-golang-jwt.md) | `auth/jwt` wraps `golang-jwt/jwt/v5` | Accepted |
| [0028](0028-realtime-rooms-use-a-single-owner-event-loop-not-locks.md) | Realtime rooms use a single-owner event loop, not locks | Accepted |
| [0029](0029-ratelimit-is-a-concrete-token-bucket.md) | `ratelimit` ships a concrete token bucket, no storage interface yet | Accepted |
| [0030](0030-lifecycle-is-explicit-registry-registration-not-an-optional-app-interface.md) | Lifecycle components register explicitly through `Registry`, not an optional `App` interface | Accepted |
| [0031](0031-shutdown-uses-two-independent-phase-timeouts-and-a-decoupled-application-context.md) | Graceful shutdown uses two independent per-phase timeouts and a context decoupled from the caller's cancellation | Accepted |
| [0032](0032-scheduler-is-one-framework-owned-lifecycle-invisible-to-registry-lifecycles.md) | The scheduler is one framework-owned Lifecycle, appended last and invisible through `Registry.Lifecycles()` | Accepted |
| [0033](0033-jobs-use-skip-on-overlap-concurrency-with-no-built-in-retry.md) | Jobs use fixed skip-on-overlap concurrency, no built-in retry, and silent skipped-tick diagnostics | Accepted |
| [0034](0034-job-failure-reporting-suppresses-only-application-context-cancellation.md) | Job failure reporting suppresses only errors matching the Application context's own cancellation | Accepted |
| [0035](0035-slog-is-the-logging-primitive-with-explicit-non-global-injection.md) | `*slog.Logger` is tanGO's logging primitive, injected explicitly per component, never as global mutable state | Accepted |
| [0036](0036-recorder-is-a-minimal-two-instrument-interface-with-no-bundled-exporter.md) | `observability.Recorder` is a minimal two-instrument interface, low-cardinality attributes only, no bundled exporter | Accepted |
| [0037](0037-request-id-never-trusts-an-inbound-header-without-explicit-opt-in.md) | `RequestID` middleware never trusts an inbound header unless a host explicitly opts in | Accepted |
| [0038](0038-null-reads-as-the-zero-value-and-unset-foreign-keys-write-null.md) | NULL reads as the zero value, and an unset foreign key is written as NULL | Accepted |
| [0039](0039-secure-cookies-trust-forwarded-proto.md) | Cookies' Secure flag trusts the proxy's forwarded protocol | Accepted |
| [0040](0040-tests-run-once-per-test-dialect-chosen-by-one-scheme-driven-dsn.md) | Tests run once per Test dialect, chosen by one scheme-driven `TANGO_TEST_DSN` through a public `testdb` package | Accepted |
| [0041](0041-renames-and-drops-are-explicit-makemigrations-flags.md) | Renames and drops are explicit `makemigrations` flags (`--rename`, per-item `--allow-drop`), and drops are refused by default | Accepted |
