# FK referential-integrity check is a non-transactional preflight, and treats zero as unset

`Store.Create`/`Update` now validate that a set foreign key field actually references an existing row (returning `db.ErrInvalidForeignKey`), distinct from the schema-level `ValidateForeignKeys` check that only confirms the *related model* exists. Two things about this check are deliberate, not oversights:

1. **No shared transaction with the write.** The check is a plain preflight `SELECT`, not wrapped in the same `sql.Tx` as the subsequent `INSERT`/`UPDATE`. This leaves a narrow TOCTOU race — the referenced row could be deleted between the check and the write — but the generated DB-level `REFERENCES` constraint (see [ADR 0011](0011-fk-target-validated-after-registration-not-at-register-time.md)) already closes that race when enabled (always on Postgres; on SQLite when `db.SQLiteForeignKeysDSN` is used). This check exists to turn an opaque driver error into a named, `errors.Is`-checkable one for the common case, not to be the sole guarantee of integrity — paying for transactional atomicity here would defend against a race the database already defends against.

2. **A foreign key field left at Go's zero value (`0`) is treated as "unset" and skipped**, rather than validated as a reference to primary key `0`. tanGO has no nullable-field mechanism, so this is a narrow convention riding on the fact that every example's primary keys start at `1`. A model that deliberately assigns `0` as a real primary key silently defeats this check on any field referencing it.

Considered and rejected: wrapping the check in a shared transaction (adds cost and complexity for a race already covered by the DB constraint when the constraint is enabled); validating `0` strictly (would make every FK field on a Go struct implicitly mandatory, a bigger scope change than the narrow hardening this check was meant to be).

**Amended by [ADR 0038](0038-null-reads-as-the-zero-value-and-unset-foreign-keys-write-null.md):** a zero foreign key is still skipped by this check, and is now written to the database as `NULL` rather than `0`.
