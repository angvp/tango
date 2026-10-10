# The transaction seam is a Store bound to one transaction, with flat nesting

A registration has to create an `Account` and the host's own rows (a profile, say) together, or not at all. `Store.InTx(ctx, fn)` is that seam: `fn` receives a `*db.Store` bound to one transaction, so every method keeps its validation (bounded strings, foreign keys) and no second CRUD API exists. `InTx` commits when `fn` returns nil, rolls back when it returns an error (returned as is), and on a panic rolls back and re-panics with the same value, so a programmer error is never turned into a returned error. `accounts` uses it for `OnRegister`, and so can any host.

Nesting is flat: `InTx` on a store that is already in a transaction joins it, with no savepoints. An inner error rolls the work back only if it propagates out of the outermost callback; an outer callback that swallows it can still commit the inner writes. Operations that open their own transaction when called directly (a PostgreSQL insert with an explicit ID, a cascading `Delete`) run on the enclosing one, so the table lock is held until it ends.

Rejected:

- **`Begin`/`Commit` handles on `Store`.** They invite a forgotten rollback and a leaked connection; a callback makes both impossible.
- **Savepoints for nesting.** SQLite and PostgreSQL differ in the details, the need is unproven, and flat joining is simple to state. A host that needs partial rollback can add it with raw SQL.
- **A context-carried transaction.** The outer store would silently join a transaction nobody sees in the code.
- **Passing a `*sql.Tx` to the hook.** It would bypass the validation the hook's rows need.

Consequences: code inside `fn` must use `tx`, not the outer store, which is another connection and does not see the transaction's writes; a long callback holds a connection and, on PostgreSQL, any lock it took; swallowing an inner error is the caller's choice and is documented. `Store.InTx` joins the Covered API.
