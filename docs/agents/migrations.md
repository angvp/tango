# Agent Recipe: Migrations

Use this after adding or changing models.

Canonical examples: `examples/api-with-admin/migrations/0001_auto.go`, `examples/api-with-admin/migrations/0005_add_admin_staff_and_superuser.go`, and `examples/api-with-admin/migrations/migrations.go`.

## Do

- Run the project's makemigrations workflow after model changes.
- Inspect the generated migration before applying it.
- Confirm the migration's `App` and `Name` are correct.
- Confirm generated columns, indexes, uniqueness, and foreign-key references match the Go structs.
- Apply migrations through the project's migrate workflow only after review.

Typical project commands:

```sh
go run . -tango-dump-models
tango makemigrations
go run . -migrate
```

Use the actual project workflow if it wraps these commands differently.

## Don't

- Do not hand-edit generated migration files unless the change is intentional and reviewed.
- Do not leave an accidental migration diff after exploratory model changes.
- Do not assume `makemigrations` applied anything; generation and application are separate steps.

## Check

- Compare migration structure with `examples/api-with-admin/migrations/`.
- Run the shared checklist: `docs/agents/checklist.md`.
