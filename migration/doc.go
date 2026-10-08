// Package migration applies and rolls back the schema migrations
// `tango makemigrations` generates.
//
// An application hands its [Migration] values to [ApplyPending] and
// [RollbackLast], which record what has run in the tango_migrations table;
// a generated project's main does this through tango.DispatchFlags.
// [EnsureTrackingTable], [AppliedMigrations], [IsMissingTrackingTable] and
// [MigrationKey] read that record.
//
// The step types ([Step], [Column], [CreateTable] and the other steps),
// [Model], [SchemaState] and its parts, the diff and replay functions, and
// [ApplyStep] exist for generated migration files and the tango CLI. They
// are not covered by tanGO's compatibility promise and may change in any
// minor release, with one exception, the Generated-file contract: a
// migration file written by a released `tango makemigrations` keeps
// compiling, being read by later runs, applying and rolling back on every
// later v0.x release. See
// https://tangoframework.com/docs/compatibility/ and
// https://tangoframework.com/docs/guides/migrations/.
package migration
