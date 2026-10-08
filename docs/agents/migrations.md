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
- When a field or model was renamed, pass `--rename app.Model.Field=NewField` or `--rename app.Model=NewModel` so its data is kept; never `--allow-drop` it. Name a renamed model's fields by the model's old name.
- Only widening type changes are generated (`integer`→`real`/`text`, `real`→`text`, `boolean`→`integer`/`text`); for any other, add a new field, copy the data, then rename and drop, rather than editing the generated migration.
- Do not pass `--allow-drop` to get past a refused run unless the model or field really should be deleted with its data; each `--allow-drop` names exactly one `app.Model` or `app.Model.Field`.

## Check

- Compare migration structure with `examples/api-with-admin/migrations/`.
- Run the shared checklist: `docs/agents/checklist.md`.
