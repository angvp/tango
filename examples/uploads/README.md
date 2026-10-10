# Uploaded files

A document locker. Signed-in accounts upload a file, list their files, download them again and delete them. It shows the path tanGO's `storage` package expects a host to write:

- **`storage.Upload`** reads one multipart file, streaming it into the object store with a size cap and a content-type allowlist. The type is sniffed from the bytes; the type the browser declares is never used.
- **The host keeps the key and the metadata** in its own model, `Document` (`apps/documents`), with bounded strings (`varchar=n`, see [ADR 0049](../../docs/adr/0049-bounded-strings-are-declared-by-tag-and-validated-by-rune-count-in-go.md)). The generated key is never shown to a client and never decides access: the owner check does.
- **Bytes first, then the row, in a transaction.** If the row cannot be written, the new object is deleted. Deleting a document removes the row first and the bytes best-effort, so a failure leaves an unreachable object, never a row pointing at nothing.
- **`storage.Serve`** delivers a download the View has already authorized: attachment by default, `nosniff`, ranges and conditional requests. `?inline=1` shows an image in the browser; any other type stays an attachment.
- **Body limits:** every route that carries no file keeps a 64 KiB limit; the upload route has its own, larger one, outside the small one.

## Run it

```sh
git clone https://github.com/angvp/tango
cd tango/examples/uploads
go run . -migrate
go run .
```

Files are kept under `UPLOADS_DIR` (default `./uploads`) by `storage/local`, which suits development and a single host with a persistent volume. Create an account at `/accounts/register/`, then, with its session cookie:

```sh
curl -b cookies.txt -F file=@photo.png localhost:8000/documents/
curl -b cookies.txt localhost:8000/documents/
curl -b cookies.txt -OJ localhost:8000/documents/1/download/
curl -b cookies.txt -X DELETE localhost:8000/documents/1/
```

## Things to know

- Local disk is not shared between instances and does not outlive a container with no volume. For that, use [`storage/s3`](../../docs/guides/uploads.md#choosing-a-backend) (Amazon S3, Cloudflare R2 or another S3-compatible provider) instead of `storage/local`; only `main.go` changes.
- tanGO ships no sweeper, virus scanning or CDN. See the [uploads guide](../../docs/guides/uploads.md).
- `go test .` runs the whole flow against an in-memory object store (`storagetest.NewMemory`), on SQLite or, with `TANGO_TEST_DSN`, PostgreSQL.
