# Migrations as typed Go steps, not raw SQL files

## Context

Schema migrations need a file format. tanGO supports two SQL dialects (SQLite and PostgreSQL), and the two differ meaningfully in DDL capability — SQLite can't `ALTER TABLE ... DROP COLUMN` reliably across all versions and has no `ALTER COLUMN` at all, while Postgres supports both directly.

Two candidate migration file formats:

1. **Raw SQL files.** Simple, auditable, standard in many frameworks (Rails, golang-migrate). But since SQLite and Postgres need genuinely different DDL for the same logical change (e.g. dropping a column), this would require either one file per dialect per migration (duplication that can drift out of sync), or giving up on one of the two dialects per migration.
2. **Generated Go source expressing typed steps** (`CreateTable`, `AddColumn`, `DropColumn`, etc.), with dialect-specific DDL translation living once in a shared runner. A migration file describes *what* changed, not *how* to express it in a specific dialect's SQL.

## Decision

Migration files are generated Go source containing a list of typed steps. Translating a step into dialect-specific DDL (including SQLite's table-rebuild pattern for operations it can't do via direct `ALTER TABLE`) is the runner's job, done once, driven by the `Dialect` value from [ADR 0004](0004-explicit-dialect-for-postgresql-support.md).

## Consequences

- One migration file works for every supported dialect — no per-dialect duplication, and no risk of a SQLite variant and a Postgres variant of the same migration silently drifting apart.
- The step vocabulary is deliberately small and matches exactly what `ModelMeta`/`FieldMeta` can express (including the `index` tag, added for this purpose). Anything `ModelMeta` can't describe (renames, type changes) can't be expressed as a step either — a real limitation, not an oversight, and one worth revisiting if `ModelMeta` itself grows richer.
- The runner carries real complexity for SQLite's table-rebuild pattern (create new table, copy data, drop old, rename) — more code than "write the `ALTER TABLE` and run it," but it's centralized in one place rather than spread across every migration file a project accumulates.
- A hand-auditor reading a migration file sees typed steps (`DropColumn{Table: "user", Column: "legacy_field"}`), not literal SQL — readable, but a developer used to raw-SQL migration tools needs to learn tanGO's step vocabulary instead of relying on general SQL knowledge. This is an accepted cost of dialect portability.
- Extending to a third dialect (e.g. MySQL) later means teaching the runner how to translate the existing step vocabulary into MySQL DDL — no migration file needs to change.
