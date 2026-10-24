package gcs

import (
	"context"
	"io"
	"net/http"
	"os"

	"google.golang.org/api/option"
)

// SetKeyFunc makes the Store take its generated keys from next, so a test can
// force a collision.
func (s *Store) SetKeyFunc(next func() string) { s.nextKey = next }

// NewWithHTTPClient is New with the SDK client's transport supplied by the
// test, so it can count and perturb the requests the adapter sends.
func NewWithHTTPClient(ctx context.Context, cfg Config, hc *http.Client) (*Store, error) {
	return newStore(cfg, option.WithHTTPClient(hc))
}

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
