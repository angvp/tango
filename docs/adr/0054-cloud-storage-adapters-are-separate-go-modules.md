# Cloud storage adapters are separate Go modules

`storage/s3` needs the AWS SDK for Go v2, which brings dozens of modules into `go.mod` and `go.sum`. A subpackage in the root module would keep the SDK out of a project's build, but not out of its module graph, its `go.sum`, or its vulnerability and license scans, and a project that stores files on local disk would still carry all of it. tanGO's root module has stayed small on purpose.

`storage/s3` is therefore its own Go module, `github.com/angvp/tango/storage/s3`, with its own `go.mod`, requiring the root module and the SDK. Core `storage`, `storage/local` and `storagetest` stay in the root module and add no dependency. Adapters release on their own tags (`storage/s3/vX.Y.Z`) and may lag the core. Every adapter must pass the same conformance suite, `storagetest.Run`, so the `Store` contract is one executable specification rather than one per backend. The S3 adapter covers Amazon S3, Cloudflare R2 and other S3-compatible providers through `Endpoint` and `PathStyle`, not through separate adapters. It tests against a small in-process S3 server in CI and against a real bucket when `TANGO_TEST_S3_ENDPOINT` is set.

Native Google Cloud Storage is deferred to a later release as `storage/gcs`, in its own module on the same terms; the `Store` interface stays small enough that it needs no change.

Rejected:

- **Subpackages of the root module.** They leak the SDK into every project's module graph.
- **One adapter module for all clouds.** Choosing R2 would still pull in the Google SDK.
- **A heavyweight blob-store abstraction library.** It would add a dependency and an API tanGO does not control.
- **Direct-to-cloud and resumable uploads.** They move the size, type and authorization checks away from the host's View.

Consequences: an adapter has its own release and tag process (documented in the release notes for the version that ships it); `go test ./...` at the repository root does not run an adapter's tests, so CI runs each module; and an adapter's configuration struct is Covered in its own module under the same policy.
