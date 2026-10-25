# Uploaded files

A document locker. Signed-in accounts upload a file, list their files, download them again and delete them. It shows the path tanGO's `storage` package expects a host to write:

- **`storage.Upload`** reads one multipart file, streaming it into the object store with a size cap and a content-type allowlist. The type is sniffed from the bytes; the type the browser declares is never used.
- **The host keeps the key and the metadata** in its own model, `Document` (`apps/documents`), with bounded strings (`varchar=n`). The generated key is never shown to a client and never decides access: the owner check does.
- **Bytes first, then the row, in a transaction.** If the row cannot be written, the new object is deleted. Deleting a document removes the row first and the bytes best-effort, so a failure leaves an unreachable object, never a row pointing at nothing.
- **An admin changelist** (`apps/documents/admin.go`) lists every file with its size (`admin.ByteSize`), a shortened digest (`admin.ShortHash`) and a download link. The link is the same owner-only download route the API uses, so a staff session alone does not download a file. The admin's edit form never shows the storage key (`apps/documents/admin_widget.go` renders a note instead), keeps it on save, and cannot repoint a row at another object. Upload metadata (owner, size, digest, type, time) is read-only there, and the form cannot create a file: uploads go through the upload route only. Staff can rename a file.
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

The admin is at `/admin/`. Create a staff account with the admin CLI, then sign in at `/admin/login/` to see the Documents changelist:

```sh
go run . -tango-admin-create=staff
```

The list links each file to `/documents/{id}/download/`, which still asks for the owning account's session. Signing in as staff does not open another account's file.

## Things to know

- Deleting a file in the admin removes its row but leaves its stored bytes behind, since admin has no hook into the host's storage. Delete through `DELETE /documents/{id}/` to remove both.
- Local disk is not shared between instances and does not outlive a container with no volume. For that, use [`storage/s3`](../../docs/guides/uploads.md#choosing-a-backend) (Amazon S3, Cloudflare R2 or another S3-compatible provider) instead of `storage/local`. Swapping backends changes only `main.go`, plus the module requirement for the backend you choose. This example has no Google Cloud code or dependency; `storage/gcs` is unreleased, and its [guide section](../../docs/guides/uploads.md#google-cloud-storage-experimental-separate-module) is the reference for that backend.
- tanGO ships no sweeper, virus scanning or CDN. See the [uploads guide](../../docs/guides/uploads.md).
- `go test .` runs the whole flow against an in-memory object store (`storagetest.NewMemory`), on SQLite or, with `TANGO_TEST_DSN`, PostgreSQL.
