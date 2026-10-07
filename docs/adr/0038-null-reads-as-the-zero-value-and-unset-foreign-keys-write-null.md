# NULL reads as the zero value, and an unset foreign key is written as NULL

tanGO model fields are plain Go values, and its one relationship convention already says a foreign key left at `0` is unset. The database didn't agree on either point. A migration that added a column left existing rows `NULL`, and scanning `NULL` into an `int64` or `string` field failed, so a model couldn't read its own rows after gaining a field. A foreign key left at `0` was written as `0`, which the column's `REFERENCES` constraint rejects, so "unset" only held inside `Store`'s own preflight check.

`db.Store` now treats `NULL` and the zero value as the same thing in both directions. Every read (`Get`, `List`, `Query`, `QueryRow`) scans `NULL` into the field's zero value, and `Create`/`Update` write a zero foreign key as `NULL`. Pointer and `sql.Null*` fields in raw-SQL destinations keep their own meaning, so code that needs to tell `NULL` from zero still can.

We considered two alternatives. Nullable model fields (pointers or `sql.Null*`) would be more precise, but they push `nil` checks into every view and template for a distinction most apps don't need; they can still come later as an addition. Having `makemigrations` backfill every added column with its zero value would fix old rows but not the foreign key case, since `0` isn't a valid reference, and it would make `Column.Default` a general default system, which it deliberately isn't.

The cost is one ambiguity: a `Where` condition on a zero value doesn't match `NULL` rows, because SQL's `=` never matches `NULL`. Filtering for unset values needs raw SQL with `IS NULL`.

This amends the "zero means unset" convention in [ADR 0012](0012-fk-referential-integrity-check-is-a-non-transactional-preflight.md): the preflight still skips a zero foreign key, and the database now stores it as `NULL` instead of `0`. The `IS NULL` boundary from [ADR 0024](0024-query-filtering-is-a-bounded-where-primitive.md) still holds.
