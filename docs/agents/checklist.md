# Agent Validation Checklist

Use this shared checklist from every recipe instead of repeating validation steps inline.

## Code Validation

- Run `go test ./...` in the project module you changed.
- Run `go vet ./...` when the project has no known vet blockers.
- Run `gofmt` on changed Go files.

## tanGO Validation

- Run the app's `tango check` path, usually `go run . -check`.
- After model changes, run `tango makemigrations` through the project workflow and inspect the generated migration before applying it.
- Confirm the migration diff is expected; do not keep an accidental `*_auto.go` change.
- Run `tango migrate` through the project workflow, usually `go run . -migrate`.

## Review

- Check that only public tanGO APIs are imported; never import `internal/`.
- Check that registration is explicit through `InstalledApps` and app `Register` functions.
- Check that docs or prompts reference canonical examples instead of copying full files.
