# Generated-file contract fixtures

Each directory here holds the migration files one tanGO release's own `tango makemigrations` wrote, unedited. The [Generated-file contract](../../docs/compatibility.md#the-generated-file-contract) promises they keep working on every later v0.x release, and two tests hold it to that on SQLite and PostgreSQL:

- `compat_test.go` in this directory compiles every version's files, applies them one at a time to a fresh database, and rolls back and reapplies each reversible one.
- `generated_files_test.go` in `internal/cli` checks that each file's `// tango:migration-json` header decodes to exactly the steps its Go literal compiles to. It also checks that `tango makemigrations`, given the release app's recorded `-tango-dump-models` output (`models.json`), finds nothing left to generate.

| Directory | Written by |
|---|---|
| `v0_0_1` | `tango` v0.0.1 |
| `v0_0_2` | `tango` v0.0.2 |
| `v0_1_0` | `tango` v0.1.0 |
| `v0_2_0` | `tango` v0.2.0 |
| `unreleased` | this checkout's generator, for the step kinds no release has emitted yet |

## Regenerating

`generate.sh` builds a small app against one tanGO version, evolves its models through a fixed series of stages, and runs that version's own CLI after each stage. The stages cover:

- creating tables with unique, indexed, foreign-key and timestamp columns;
- adding columns;
- changing unique and index flags;
- dropping an index, a column and a table;
- renaming a field and a model, and a widening type change (only on releases that support them);
- a bounded string (`tango:"varchar=n"`) added, narrowed from text, widened and made text again (only on v0.4.0 and later, and `unreleased`).

Then it replaces the version's directory here.

```sh
./generate.sh v0.0.2               # a released version, fetched through the Go module proxy
./generate.sh v0.1.0 local         # this checkout, saved as v0.1.0: run before tagging v0.1.0
./generate.sh unreleased local     # after changing what the generator writes
```

Adding a release's output is a [release checklist](../../RELEASING.md) step, and the Release workflow refuses a tag whose directory is missing or not listed in `Generators`. When you add a new directory, add it to `Generators` in `versions.go`, which both tests read. Never edit a released version's files by hand: they stand for what that release's users have in their repositories.
