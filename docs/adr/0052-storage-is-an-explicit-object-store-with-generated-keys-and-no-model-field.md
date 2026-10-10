# Storage is an explicit object store with generated keys and no model field

Accepting a file involves five separate concerns: multipart intake, durable bytes, relational metadata, authorization and HTTP delivery. A Django-style `FileField` would hide all of them behind a model tag, and in Go that tag would also hide where the bytes live, who may read them and what happens when a row changes. `storage` instead gives a host one small interface and two helpers, and leaves the rest where it already is.

`storage.Store` has `Put`, `Stat`, `Open` (a byte range) and `Delete`. `Put` streams from an `io.Reader`, is atomic and create-only, enforces a required size cap, always sniffs the content type from the bytes, applies an optional allowed-types policy, and returns a key it generated, with the size and SHA-256. The policy lives in `PutOptions`, not only in the multipart helper, so calling `Put` directly cannot bypass it. A key is 160 random bits in a restricted alphabet; `Put` takes no key and no filename, and every method refuses any string not in that form, so a client filename can never become a path or an object name. The host keeps the key and metadata in its own model (with bounded strings, [ADR 0049](0049-bounded-strings-are-declared-by-tag-and-validated-by-rune-count-in-go.md)), writes the row in a transaction ([ADR 0051](0051-the-transaction-seam-is-a-store-bound-to-one-transaction.md)), and authorizes in its own View. `storage.Upload` reads one multipart file with bounded memory. `storage.Serve` delivers an authorized object: attachment by default, inline only for listed non-active types, `nosniff`, ranges and conditional requests, the sniffed type and never the client's.

Reading is `Stat` plus a ranged open that returns an `io.ReadCloser`, not a seekable reader, so `Serve` does its own range arithmetic and S3 never hides costly re-requests behind `Seek`. There is no listing: pagination, consistency and cost on a cloud store are real design work that no consumer needs yet, and a host that must find stray objects keeps its own records. The store, `Upload` and `Serve` never log, because a size refusal, a missing object or an authorization failure is an ordinary outcome and the host's View is the only place that knows the business context.

Rejected:

- **A `FileField` or model tag.** It couples the model layer to storage, hides authorization, and makes migrations and deletes responsible for bytes.
- **A shipped file model.** It would be a new Covered table in every project's migrations and an admin surface to keep stable.
- **Caller-chosen keys or filenames as keys.** They invite path traversal, collisions and guessable names.
- **A seekable `Open`.** It maps neatly onto `http.ServeContent` but turns each seek on S3 into a request.
- **Signed URLs and a public static route.** They are cloud-specific and move the authorization decision out of the host's View.
- **Framework-level storage logging.** Noisy for ordinary outcomes and duplicates the host's own.

Consequences: the host writes an upload View, a metadata model and a download View (about a screen of code, shown in `examples/uploads`); orphan cleanup is the host's, with a documented safe order; there is no sweeper or virus scan; and the `storage` surface, its errors and `Serve`'s headers join the Covered API.
