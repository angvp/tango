# Guide: file uploads and storage

`github.com/angvp/tango/storage` keeps uploaded bytes outside your database. It is deliberately small: an object store you call from ordinary Views. There is no `FileField`, no model tag and no admin widget. You keep the metadata in your own model, you authorize every access in your own View, and tanGO's two helpers (`storage.Upload`, `storage.Serve`) do the parts that are easy to get wrong.

The runnable proof is [`examples/uploads`](../../examples/uploads).

## The pieces

| Piece | What it is |
|---|---|
| `storage.Store` | The object store: `Put`, `Stat`, `Open` (a byte range), `Delete`. No listing. |
| `storage.Upload` | Reads one file from a multipart request and `Put`s it. |
| `storage.Serve` | Delivers an object to a request your View has already authorized. |
| `storage/local` | A filesystem `Store`. |
| `storage/s3` | An S3-compatible `Store`, in its own Go module. |
| `storage/gcs` | A Google Cloud Storage `Store`, in its own Go module. Unreleased; see [Google Cloud Storage](#google-cloud-storage-unreleased). |
| `storagetest` | `NewMemory()` for tests, and `Run`, the conformance suite every `Store` passes. |

## Choosing a backend

| | `storage/local` | `storage/s3` | `storage/gcs` (unreleased) |
|---|---|---|---|
| Use it for | Development, tests, and one host with a persistent volume | More than one instance, or a disk that does not outlive the container | The same as `storage/s3`, on Google Cloud |
| Shared between instances | No | Yes | Yes |
| Dependencies | None beyond tanGO | The AWS SDK for Go v2, in its own module | The Google Cloud Storage SDK, in its own module |
| Needs credentials | No | Yes | Yes, unless `Endpoint` names an emulator or test server |

```go
objects, err := local.New("/var/lib/myapp/uploads") // directories 0700, files 0600
```

`local.New` creates the directory private. If the root, or its `objects` or `tmp` subdirectory, already exists with any group or other permission (a `0755` volume mount, say), `New` fails with an error that names the directory and tells you to `chmod 700` it. It never changes an existing directory's permissions for you.

```go
objects, err := s3.New(ctx, s3.Config{
	Bucket:   "myapp-uploads",
	Region:   "auto",
	Endpoint: "https://<account>.r2.cloudflarestorage.com", // omit for Amazon S3
	Prefix:   "documents",                                  // optional
	// Credentials is nil: the SDK's standard chain (environment, shared config, role).
})
```

`storage/s3` is a separate module, `github.com/angvp/tango/storage/s3`, so a project that never imports it never sees the AWS SDK in its `go.mod`, `go.sum` or vulnerability scan. `Endpoint` and `PathStyle` select Cloudflare R2 or any other S3-compatible provider. Because S3 fixes an object's metadata when the upload starts and the SHA-256 is only known at the end, `Put` spools the bytes to a temporary file (`Config.TempDir`, default the system temp directory) before uploading them. Memory stays bounded, but you need disk space for one upload at a time.

## Google Cloud Storage (unreleased)

`storage/gcs` (`github.com/angvp/tango/storage/gcs`) is a `Store` on Google Cloud Storage. It is unreleased, and its release is not scheduled. Its release will carry its own tag, `storage/gcs/vX.Y.Z`, and needs the maintainer's approval. Do not add it to a project until it is released. Until then, the two ways to put uploads in GCS are:

| Path | Use it when | Configure with |
|---|---|---|
| Native: `storage/gcs` (unreleased) | The application runs on Google Cloud and authenticates with application-default credentials or workload identity | `gcs.New`, with no credentials named |
| S3-compatible: `storage/s3` (released) | You authenticate with HMAC keys | `s3.New`, with `Endpoint` set to Google's S3 interoperability endpoint and HMAC keys in `Credentials` |

tanGO's tests for `storage/s3` do not run against Google's interoperability endpoint, so check that path against your bucket before you rely on it.

```go
store, err := gcs.New(ctx, gcs.Config{
	Bucket: "myapp-uploads",
	Prefix: "documents", // optional
	// No credentials named: application-default credentials, workload identity,
	// or the file named by GOOGLE_APPLICATION_CREDENTIALS.
})
```

`CredentialsFile` or `CredentialsJSON` replace application-default credentials explicitly. Naming both is refused, and so is naming either with `Endpoint`.

To supply the client yourself, for example one with a custom transport, build it with `cloud.google.com/go/storage` and pass it to `NewFromClient`:

```go
client, err := storage.NewClient(ctx) // cloud.google.com/go/storage, built by the host
store, err := gcs.NewFromClient(client, gcs.Config{Bucket: "myapp-uploads"})
```

`NewFromClient` uses the client you built, and you own it. It refuses `Endpoint` and the credential fields, because the client already carries them. `Bucket`, `Prefix`, `ChunkSize` and `TempDir` still apply.

**Endpoint.** `Config.Endpoint` points the Store at a non-Google server, such as an emulator or a test server. A non-empty `Endpoint` never authenticates. The SDK's `STORAGE_EMULATOR_HOST` variable is ignored, so an emulator is reached only through `Endpoint`.

**Construction is lazy.** `New` validates the configuration and makes no request. The SDK client, and with it the credential lookup, is built on first use, so a missing credential or an unreachable endpoint shows on the first call, not at startup. To fail at startup instead, make one call, such as `Stat`, and treat any error other than `storage.ErrNotFound` as fatal. The context passed to `New` is not kept.

**Upload size and spooling.** `Put` spools each upload to `Config.TempDir` (default the system temp directory) before it uploads, because GCS fixes an object's metadata when its upload starts and the SHA-256 is known only at the end. The spool is bounded by `PutOptions.MaxSize` and is removed when `Put` returns. `Config.ChunkSize` bounds the memory one upload request holds. Zero means `gcs.DefaultChunkSize` (8 MiB). Otherwise it must be a positive multiple of 256 KiB, and any other value fails `New`.

**Close.** `Close` closes the client the Store built and makes later calls fail with an error. For a client passed to `NewFromClient`, `Close` does nothing, and the Store stays usable. Do not call `Close` while another method of the Store is running.

**Errors.** Match them with `errors.Is`. The adapter maps these conditions:

| Condition | Returned as |
|---|---|
| The object does not exist (`Stat`, `Open`) | `storage.ErrNotFound`. An object with no valid SHA-256 is also reported as not found. `Delete` of a missing object returns nil. |
| The requested byte range cannot be satisfied | `storage.ErrInvalidRange` |
| The stream exceeds `PutOptions.MaxSize` | `storage.ErrTooLarge`, before anything is uploaded |
| The generated key is taken by different bytes on every bounded attempt | `storage.ErrExists` |
| Any other SDK failure | `storage/gcs: <operation>: <SDK error>`, with the SDK error kept in the chain |

`Put` picks a new key when the generated one is taken by different bytes. It returns `storage.ErrExists` only after a bounded number of such collisions in a row.

The SDK error is always preserved, so `errors.Is` and `errors.As` can reach it. `errors.As` to `*googleapi.Error` is guaranteed only for the client the adapter builds, which uses HTTP/JSON. A client passed to `NewFromClient` may use its own transport, such as gRPC. Its failures then arrive as that transport's error, for example a gRPC status error, and `*googleapi.Error` does not match them. Put's create-only conflict handling also recognizes only an HTTP 412 `*googleapi.Error`, so with an injected client that reports a conflict another way, `Put` returns the wrapped error rather than confirming an earlier write or choosing a new key. Only the HTTP/JSON path is tested.

Credential-construction errors carry no wrapped chain, so `errors.Is` and `errors.As` do not reach the SDK or operating-system error behind them. This covers reading `CredentialsFile`, parsing `CredentialsJSON` or the file's contents, the application-default credentials lookup, and building the SDK client. Errors from parsing a credentials document are redacted: no credential value appears in them. The file-read error names the path, never the contents.

SDK messages can name the bucket or the object. Do not render them to users. The Store logs nothing itself; if you need the detail, record it on the server with the request's context.

## The write contract

`Put` streams from an `io.Reader`, generates the key, and returns a `storage.Object` (key, size, SHA-256, sniffed content type) that you copy into your own model.

- **The key is generated, not chosen.** `Put` takes no key and no filename; the key is 160 random bits in 32 lowercase characters. A client filename can never become a path or a key. Every method refuses any string that is not in that form with `storage.ErrInvalidKey` before it touches storage.
- **A write is atomic and create-only.** An object is either fully visible or absent, and a write never replaces an existing one.
- **The size cap is part of the write.** `PutOptions.MaxSize` is required and positive; going over fails with `storage.ErrTooLarge` and stores nothing.
- **The content type is sniffed from the bytes**, never taken from the client, and returned. `PutOptions.AllowedTypes` (`"image/png"` or `"image/*"`) refuses anything else with `storage.ErrTypeNotAllowed`, also storing nothing. Because the policy is part of `Put`, calling `Put` directly cannot bypass it.

Match errors with `errors.Is`; `errors.As` with `*storage.TooLargeError` or `*storage.TypeNotAllowedError` gives the limit or the detected type. A `Store` never logs: it returns errors and you decide what is worth recording.

## Reading a multipart upload

```go
file, err := storage.Upload(objects, ctx.Request(), storage.UploadOptions{
	PutOptions: storage.PutOptions{MaxSize: 4 << 20, AllowedTypes: []string{"image/*", "application/pdf"}},
})
switch {
case errors.Is(err, storage.ErrTooLarge):
	// 413
case errors.Is(err, storage.ErrTypeNotAllowed):
	// 415
case err != nil:
	// ErrNotMultipart, ErrNoFile, ErrMultipleFiles and ErrTooManyParts are the client's 400s
}
```

`Upload` reads the `file` field (set `UploadOptions.Field` for another), streams it into `Put` without buffering the whole upload, accepts exactly one file, and ignores other form fields beyond a small bound. Its result carries `Filename` (a sanitized display name) and `DeclaredType` (what the client claimed). Both are plain metadata for your model: neither is ever used as a key, a path, a validation input or an authorization decision.

### Body limits

`tango.MaxBodySize` can only be lowered by a route closer to the View, never raised. Put the large limit on the upload route and a small one on the routes that carry no file, instead of one strict global limit:

```go
tango.Path("POST", "/", upload, tango.Use(tango.MaxBodySize(4<<20+64<<10))),
tango.Path("GET", "/{id}/download/", download, tango.Use(tango.MaxBodySize(64<<10))),
```

A strict limit set further out, as global middleware or on an enclosing group, still applies to the upload route, which cannot raise it: keep the large limit on the upload route and do not put a small one around it. `Upload` also enforces its own per-file cap, so a generous route limit never buffers a whole upload.

## Keep the key and the metadata in your model

```go
type Document struct {
	ID          int64  `tango:"pk"`
	OwnerID     int64  `tango:"fk=Account,index"`
	Key         string `tango:"varchar=32,unique"`
	Name        string `tango:"varchar=255"`
	ContentType string `tango:"varchar=100"`
	Size        int64
	SHA256      string `tango:"varchar=64"`
}
```

The `varchar=n` tags are [bounded strings](models-and-tags.md#bounded-strings). tanGO ships no file model, so your table is yours to shape and migrate.

### Orphans: the safe order

The bytes and the row live in two systems, so one can outlive the other. Write in this order:

1. `Upload` (the bytes), then
2. the metadata row, in a [transaction](persistence-crud-and-raw-sql.md#transactions-with-intx);
3. if the row cannot be written, `Delete` the object you just stored.

To remove a file, delete the row first, then the bytes best-effort. A failed byte deletion leaves an unreachable object, never a row that points at nothing. tanGO ships no sweeper and the `Store` has no listing, so keep your own records if you need to find and remove stray objects. Delete with a context that outlives the request (`context.WithoutCancel`), since a client that has gone away should not strand the object.

## Delivering a file

```go
doc, ok := ownedBy(account, id) // your authorization; a missing and a foreign document are both 404
return storage.Serve(ctx.ResponseWriter(), ctx.Request(), objects, doc.Key, storage.ServeOptions{
	Filename:    doc.Name,
	InlineTypes: []string{"image/png", "image/jpeg"}, // only if the page should display them
})
```

`Serve` performs no authorization: the key is not a secret to rely on, so check access first. It answers `GET` and `HEAD` with a single `Range`, `If-None-Match`, `If-Modified-Since` and `If-Range`, `ETag`, `Last-Modified`, `Accept-Ranges` and `X-Content-Type-Options: nosniff`, always with the type sniffed at upload, never one the client declared.

A download is an **attachment** by default. Inline display happens only for a type you list in `InlineTypes`, and HTML, SVG, XML and script types are always attachments, so a stored file cannot run in your site's origin. `Cache-Control` defaults to `private, no-store`; set `ServeOptions.CacheControl` to change it. A missing and an invalid key give the same `404`. `Serve` returns an error only for an unexpected store failure, without writing a response; return it from your View so the framework answers `500`.

## What tanGO does not do

- **No public URLs or signed URLs.** Every download goes through your View and `Serve`.
- **No virus scanning, thumbnails, image processing, quotas or CDN.**
- **No listing and no sweeper** (see Orphans above).
- **No resumable or direct-to-cloud uploads**, and no access to multipart-upload controls. The S3 adapter may use multipart transfer internally.
- **No automatic logging.** `Upload`, `Serve` and the stores log nothing; request-level observability around your upload and download Views is yours, because only your View knows the authorization outcome and business context.
- **No admin widget.** A metadata model registers with [admin](admin-registration.md) as ordinary fields.

For more than one instance, see [running more than one instance](running-more-than-one-instance.md#uploaded-files).

## Testing

```go
objects := storagetest.NewMemory() // no disk, no credentials
```

`storagetest.Run(t, storagetest.Factory{New: …})` runs the conformance suite against any `Store`, including your own.
