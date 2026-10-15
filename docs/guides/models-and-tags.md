# Guide: models and tags

Models are plain Go structs — no base class, no embedding, no code generation:

```go
type Post struct {
	ID        int64  `tango:"pk"`
	Title     string `tango:"unique"`
	Body      string
	CreatedAt time.Time `tango:"index"`
}
```

Register one with `registry.Models().Register(Post{})` inside your app's `Register` method.

## Tags

The `tango` struct tag takes a comma-separated list of options:

| Tag | Meaning |
|---|---|
| `pk` | This field is the primary key. Exactly one field must be tagged `pk` — zero or more than one fails `Register` with a clear error. There is no `ID`-name auto-detection: tanGO never guesses. |
| `unique` | A unique constraint. Enforced at the database level once a migration adds it (see the [migrations guide](migrations.md)'s `AlterColumnUnique` step). |
| `index` | A database index, created via the `CreateIndex` migration step. A foreign key (`fk=Model`) is indexed by default; see `noindex`. |
| `noindex` | On a foreign key only: skip the index tanGO creates for it by default. A foreign key that is also `unique` gets no second index. It cannot be combined with `index`. |
| `varchar=n` | A bounded string: at most `n` characters, `n` from 1 to 10,485,760. Only on a `string` field. See [bounded strings](#bounded-strings). |
| `text` | An explicit unbounded string, the same as a bare `string` field. Only on a `string` field; it cannot be combined with `varchar=n`. |
| `json` | A string that holds JSON text, checked whenever the model is written. Only on a `string` field; it cannot be combined with `varchar=n`, `text`, `pk`, `fk`, `unique` or `index`. See [JSON fields](#json-fields). |

Combine them: `tango:"pk,unique"` is valid, though redundant (a primary key is already unique).

## Bounded strings

A bare `string` field is `text`: any length, today and in every later release. Add `varchar=n` when the field has a real maximum, such as a title or a slug:

<!-- snippet: bounded-strings -->
```go
type Headline struct {
	ID    int64  `tango:"pk"`
	Title string `tango:"varchar=200"`
	Slug  string `tango:"varchar=80,unique"`
	Body  string `tango:"text"`
}
```

What the limit means:

- **Registration**: a malformed tag fails `Register` with `model.ErrInvalidFieldTag`, naming the model and field: `varchar` with no value, `varchar=0`, a negative or non-numeric value, a value above 10,485,760, `varchar` together with `text`, a repeated tag, or either tag on a field that is not a `string`. `FieldMeta.MaxLength` is the limit, or `0` for an unbounded field.
- **Counting**: the limit counts characters as runes, the way PostgreSQL counts `VARCHAR(n)`. `é` is one rune, an emoji is one rune, a letter followed by a combining accent is two, and each byte of invalid UTF-8 counts as one. The `maxlength` attribute the admin renders counts UTF-16 units instead, so the server is the authority.
- **Enforcement**: `Store.Create` and `Store.Update` check every bounded field before any SQL runs and return a `*db.ValueTooLongError` (matching `db.ErrValueTooLong`) carrying the model, field, limit and length. An empty string and a string of exactly `n` runes are valid. The migration creates the column as `VARCHAR(n)` on both databases and adds no `CHECK` constraint: PostgreSQL enforces the length itself, SQLite does not, so on SQLite the Go check is the only guarantee. Raw SQL (`Store.Exec`, `Query`) is outside it.
- **Why it works this way**: see [ADR 0049](../adr/0049-bounded-strings-are-declared-by-tag-and-validated-by-rune-count-in-go.md).
- **Changing it later**: see [changing a field's type](migrations.md#changing-a-fields-type). Adding or lowering a limit checks the existing data first and never truncates it.

## JSON fields

Add `json` to a `string` field that holds a JSON document, such as a user's preferences:

<!-- snippet: json-field -->
```go
type Settings struct {
	ID    int64  `tango:"pk"`
	Owner string `tango:"varchar=60"`
	Prefs string `tango:"json"`
}
```

What it means:

- **Still text.** The column is ordinary `TEXT` on SQLite and PostgreSQL, and the field is a Go `string` you read and write yourself (`json.Unmarshal([]byte(s.Prefs), &v)`). The text is stored exactly as given: no reformatting, no key reordering, no duplicate-key collapsing. tanGO does not query inside it, index it or check it against a schema.
- **Registration**: `tango:"json"` sets `FieldMeta.JSON`. `json` on a field that is not a `string`, repeated, given a value, or combined with `varchar=n`, `text`, `pk`, `fk`, `unique` or `index` fails `Register` with `model.ErrInvalidFieldTag`. A unique or indexed JSON field would compare text while looking like it compares JSON, so those are refused.
- **Empty and `null`**: the empty string is the unset value and is valid; it is stored as an empty string (a `NULL` written by other means reads back as the empty string too). Whitespace-only text is not valid JSON and is refused. The text `null` is valid JSON, is stored as those four characters and is different from unset. Any single JSON value is accepted: an object, array, string, number, `true`, `false` or `null`.
- **Enforcement**: `Store.Create` and `Store.Update` check every JSON field before any SQL runs and return a `*db.InvalidJSONError` (matching `db.ErrInvalidJSON`) with the model, field and the position of the problem. The error never contains the submitted text, so it is safe to log and to show to a client. The admin, the shell and the JSON accounts API handle it for you.
- **Reads are not checked.** `Get` and `List` return whatever is stored, so a row written by raw SQL, or before the tag existed, can still be opened and repaired. Raw SQL (`Store.Exec`, `Query`) is outside the guarantee.
- **Adding the tag to an existing field** changes no column, so it generates no migration and does not look at the rows already stored. Invalid rows stay as they are, and saving one fails until it is fixed. To find them, list the table and check each value; this works the same on both databases:

<!-- snippet: json-field-audit -->
```go
func invalidSettings(ctx context.Context, store *db.Store, meta model.ModelMeta) ([]int64, error) {
	var rows []Settings
	if err := store.List(ctx, meta, db.Query{OrderBy: []string{"ID"}}, &rows); err != nil {
		return nil, err
	}
	var bad []int64
	for _, row := range rows {
		if row.Prefs != "" && !json.Valid([]byte(row.Prefs)) {
			bad = append(bad, row.ID)
		}
	}
	return bad, nil
}
```

  For a large table, page through it with `Limit` and `Offset`. There is no SQL one-liner here because the two databases do not share a JSON-validity function.
- **Why it works this way**: see [ADR 0059](../adr/0059-json-fields-are-declared-by-tag-stored-as-exact-text-and-validated-when-the-model-is-written.md). In the admin, a JSON field gets the [JSON editor](admin-registration.md#json-fields) by default.

## Supported field kinds

- `string`, `bool`
- All signed and unsigned integer kinds (`int`, `int8`...`int64`, `uint`...`uint64`)
- `float32`, `float64`
- `time.Time`

Anything else — including an embedded/anonymous struct field — fails `Register` with a clear error identifying the offending field. There is no relationship support (foreign keys, joins); see [limitations](../limitations.md).

## Naming

`ModelMeta.Name` is the Go type's name exactly as declared (`"Post"`, not `"post"`) — this is the key you pass to `registry.Models().Get(name)` and the value that appears in `tango_migrations.app`-adjacent bookkeeping. Table and column names are a separate, automatic derivation: `db.ColumnName` lowercases and snake_cases both (`Post` → `post` table, `CreatedAt` → `created_at` column). You never see or configure this mapping directly — it's applied consistently by `db.Store` and the migration generator.

## Model ownership (`ModelMeta.App`)

A model is recorded as owned by whichever app's `Register` was running when `Models().Register` was called — set automatically by `Registry.RunRegistration` before each app's `Register` runs. This is what lets `tango makemigrations` group changes by app and write one migration file per app; you never set it yourself.
