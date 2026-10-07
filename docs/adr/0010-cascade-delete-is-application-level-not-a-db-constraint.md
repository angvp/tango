# Cascade delete happens in `Store.Delete`, not via the database's `ON DELETE CASCADE`

tanGO needed a delete behavior for a row other rows reference through a foreign key field. The obvious database-native answer is `ON DELETE CASCADE` on the generated constraint, letting SQLite/Postgres handle it. We rejected that: relying on the DB's own cascade would mean the same delete behaves identically only as long as every write path goes through the same schema, and it gives tanGO no hook to run its own bookkeeping (cycle detection, atomicity across dialects with different transaction/pragma quirks) around the cascade. Instead, `Store.Delete` finds every registered model with an `fk=` field pointing at the target (scanning `model.Registry`, the same registry `admin.Options` validation already reads), deletes their rows first, recursively, then deletes the target — all inside one `sql.Tx`, tracking visited `(model, id)` pairs to survive circular or self-referential foreign keys. This mirrors how Django's ORM actually implements `on_delete=CASCADE` (in the ORM's `Collector`, not via the database), not a Go-specific invention.

The generated DB constraint itself stays a plain `RESTRICT`/`NO ACTION` — it is never exercised on tanGO's own delete path, since `Store.Delete` already empties out dependents first; it exists purely as a backstop against raw SQL or another tool deleting a referenced row directly, bypassing `Store.Delete` entirely.

## Consequences

- `db.Store` gains its first transaction (`sql.Tx`), scoped narrowly to `Delete`'s cascade — not a general transaction API.
- A delete that cascades across many models runs as many additional `DELETE` statements as there are referencing rows; there is no batching.
- Anything that deletes rows outside `Store.Delete` (raw SQL, an external tool) gets the DB's `RESTRICT` behavior instead of a cascade — a real, documented boundary, not an oversight.
