package gcs

import (
	"context"
	"io"
	"net/http"
	"os"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/option"

	"github.com/angvp/tango/internal/storagekit"
)

// SetKeyFunc makes the Store take its generated keys from next, so a test can
// force a collision.
func (s *Store) SetKeyFunc(next func() string) { s.nextKey = next }

// NewWithHTTPClient is New with the SDK client's transport supplied by the
// test, so it can count and perturb the requests the adapter sends.
func NewWithHTTPClient(ctx context.Context, cfg Config, hc *http.Client) (*Store, error) {
	return newStore(cfg, option.WithHTTPClient(hc))
}

// TokenMetadata exposes the internal write-token metadata key to a test.
const TokenMetadata = tokenMetadata

// SetWriteTokenFunc makes the Store take its per-Put write token from next.
func (s *Store) SetWriteTokenFunc(next func() (string, error)) { s.newToken = next }

// SetSpoolSource replaces how Put reads the spooled bytes back for upload. The
// returned function restores the default.
func SetSpoolSource(next func(*os.File, int64) io.Reader) (restore func()) {
	old := spoolSource
	spoolSource = next
	return func() { spoolSource = old }
}

// SetGoogleEndpoint points the authorized client, which is what an adapter
// with no Endpoint uses, at base for the rest of a test. The returned function
// restores the default.
func SetGoogleEndpoint(base string) (restore func()) {
	old := googleEndpoint
	googleEndpoint = base
	return func() { googleEndpoint = old }
}

// RedactCredentials exposes the credential redaction for a unit test.
func RedactCredentials(msg string, raw []byte) error { return redactCredentials(msg, raw) }

// SetSpoolOps replaces the spool's file operations, so a test can fail one.
func (s *Store) SetSpoolOps(ops storagekit.FileOps) { s.spoolOps = ops }

// SetReleaseSpool replaces how Put releases its spool. A replacement should
// call the real release to keep the file from leaking.
func (s *Store) SetReleaseSpool(next func(*storagekit.SpoolFile) error) { s.releaseSpool = next }

// SetDial wraps the build of the adapter's own client, so a test can gate it or
// observe the context it receives.
func (s *Store) SetDial(wrap func(build func(context.Context) (*gcsstorage.Client, error)) func(context.Context) (*gcsstorage.Client, error)) {
	s.build = wrap(s.build)
}

// Publishes reports how many adapter-built clients the Store has kept.
func (s *Store) Publishes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishes
}
