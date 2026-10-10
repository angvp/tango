# JSON fields are declared by tag, stored as exact text and validated when the model is written

An application that keeps JSON in a string field (the spa-accounts profile's `Preferences`) had nothing stopping an invalid document being written by a View, a job or the shell. A field now says what it holds with `tango:"json"` on an ordinary Go `string`. `Store.Create` and `Store.Update` check it with `encoding/json`'s `Valid` before any SQL runs and return a typed error that matches `db.ErrInvalidJSON`, so the admin, the shell and `accounts` inherit it. The error is `*db.InvalidJSONError{Model, Field, Err}`; its message names the model and field and the syntax problem, and never the submitted text. Registration records the tag as `model.FieldMeta.JSON`.

The column is `TEXT` on SQLite and PostgreSQL, so the stored bytes are exactly the submitted bytes: no reformatting, no key reordering, no duplicate-key collapsing. `""` is the unset value (stored as `NULL`, as for every field) and is valid. Whitespace-only text is invalid. The literal `null` is valid JSON, is stored as text, and is distinct from unset. Any JSON top-level value is allowed; checking a document's shape would be schema validation, which this does not do.

Only writes through the Store are checked. Reads return whatever is stored, so a row written by raw SQL, or before the tag existed, can still be opened and repaired. Adding the tag to an existing field changes no column, so it generates no migration and scans no historical rows. The documentation says so and gives an audit recipe: a short Go loop over the table with `json.Valid`, since no version-independent SQL predicate is promised on both dialects.

A JSON field cannot be combined with `varchar=n`, `text`, `pk`, `fk`, `unique` or `index`, and `json` on a non-string field fails `Register` with `model.ErrInvalidFieldTag`. Equal documents written differently are different bytes, so a unique or indexed JSON field would compare text while looking like it compares JSON. The admin gives a JSON field `admin.JSON()` by default; an explicit `Options.Widgets` entry wins, and read-only and changelist behaviour stay generic. The JSON `accounts` API answers `*db.InvalidJSONError` with the same `422 invalid_field` as an over-long value.

Rejected:

- **A named Go type (`model.JSON`).** It is a new Covered type that callers convert by hand, while a tag matches `varchar=n` and `text` and a type can still be added later without breaking anyone. ADR 0049 deferred a model-level `JSON` type until admin semantics existed; M50 supplied them, and they need no type.
- **Byte or `json.RawMessage` fields.** Model fields have no slice kinds. Opening them touches registration, scanning and migration type tokens for no extra guarantee.
- **PostgreSQL `jsonb`.** It normalizes whitespace, key order and duplicate keys, so a read would not return what was written, and the two dialects would differ. It is also what invites operators, paths and GIN indexes, which this does not offer. A later opt-in token can add it.
- **Validating on read.** A bad row would become impossible to open and fix in the admin.
- **A `CHECK` constraint or a migration preflight.** SQLite and PostgreSQL disagree on JSON validity functions, and adding the tag changes no column. This repeats ADR 0049's reasoning.

Consequences: raw SQL and out-of-band writers bypass the check; an old invalid row stays until it is next saved, which then fails until it is fixed; there is no way to query inside a JSON field except raw SQL, which is documented as outside the Covered API.
