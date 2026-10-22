# Query filtering gains a boolean tree, IN, IS NULL, First and Exists

## Context

[ADR 0024](0024-query-filtering-is-a-bounded-where-primitive.md) and [ADR 0025](0025-query-filtering-gains-like-any-and-count.md) kept filtering to AND conditions plus one flat OR group, with no `IS NULL`, and left everything else to raw SQL. Three concrete needs now exceed that: selecting rows by a list of primary keys, filtering on an unset foreign key (stored as `NULL`, so `= 0` never matches), and grouped conditions such as `(status = published OR featured = true) AND owner_id = ?`. Guard paths also read one row with `List` and `Limit: 1`, which has no defined order.

## Decision

**`Query` stays plain data.** A new `Query.Filter *Expr` holds an inspectable boolean tree (`And`, `Or`, `Not`, `Cond`, `In`, `IsNull`, `IsNotNull`), ANDed with `Where` and `Any`, which are unchanged. There is no chainable builder. A query without a `Filter` produces byte-identical SQL, pinned by golden tests on every dialect.

**`IN` and the `NULL` operators work everywhere.** `OpIn`, `OpIsNull` and `OpIsNotNull` are usable in `Where`, `Any` and `Filter`. An empty or nil `IN` list matches nothing and renders `(1 = 0)` with no arguments, the same on every dialect (an empty `IN ()` is a syntax error on PostgreSQL and MySQL). A zero foreign-key comparison is not rewritten: `OwnerID = 0` keeps its literal SQL and the docs point to `IsNull`. `Not` is a literal `NOT (...)`, so `NULL` rows stay out of it.

**Queries are bounded and refused before any SQL.** A filter has at most 100 nodes and 8 levels, a list at most 1,000 values, and a query at most 10,000 bind values on every dialect, so a query that works on one dialect works on all. A malformed query returns an error matching `ErrInvalidQuery` (`ErrQueryLimit` for a limit); the existing `Where`/`Any` errors keep their text and also match it. An empty `And()` or `Or()` is an error, unlike an empty `IN` list, because the first is a construction mistake and the second is data.

**`First` and `Exists`.** `First` orders by the primary key unless told otherwise, so it is deterministic, and returns `ErrNotFound` naming the model. `Exists` ignores `Limit`, `Offset` and `OrderBy`. Both share one validating compiler with `List` and `Count`. Eight single-row lookups in `admin` and `accounts` moved to `First`; each filters on a unique column or primary key, so the ordering cannot change their result.

Not part of this decision: `CreateMany`, `UpdateWhere`, upsert, aggregates, relation loading, locking, soft delete and joins stay out until an application needs them. `DeleteWhere` is designed with the admin's bulk actions.
