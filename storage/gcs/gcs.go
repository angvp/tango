// Package gcs is a storage.Store on Google Cloud Storage. It lives in its own
// module so that importing storage or storage/local never brings the Google
// SDK into a project.
//
// Each object is one GCS object named Prefix+key. Its sniffed content type is
// the object's Content-Type, and its SHA-256 is custom metadata, so Stat is
// one attributes read. A write is create-only: it carries the DoesNotExist
// precondition, and a key that already exists is retried with a new one.
//
// The SHA-256 cannot be known before the stream ends, and GCS fixes an
// object's metadata when its upload starts. So Put first spools the bytes
// into a temporary file (bounded by PutOptions.MaxSize, never in memory),
// then uploads from that file.
package gcs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	htransport "google.golang.org/api/transport/http"

	"github.com/angvp/tango/internal/storagekit"
	"github.com/angvp/tango/storage"
)

const (
	shaMetadata = "sha256"
	// tokenMetadata is internal. It holds a random per-Put write token, so a
	// retry can tell its own object from a foreign object with the same SHA-256.
	tokenMetadata = "tango-write-token"

	// DefaultChunkSize is the upload chunk size when Config.ChunkSize is 0.
	DefaultChunkSize int64 = 8 << 20
	// chunkQuantum is the unit GCS requires every upload chunk to be a
	// multiple of.
	chunkQuantum int64 = 256 << 10

	cloudPlatform = "https://www.googleapis.com/auth/cloud-platform"
)

// googleEndpoint is the JSON API root of Google Cloud Storage. A variable only so
// a test can point the authorized client at a recorder.
var googleEndpoint = "https://storage.googleapis.com/storage/v1/"

var errStoreClosed = errors.New("storage/gcs: store is closed")

// Config describes the bucket a Store keeps its objects in.
type Config struct {
	// Bucket is required.
	Bucket string
	// Prefix puts every object under a "folder" of the bucket. It is made of
	// letters, digits, ".", "_" and "-" separated by "/", with no "." or ".."
	// segment.
	Prefix string
	// Endpoint is the base URL of a non-Google server, such as an emulator or
	// a test server. Empty means Google Cloud Storage with application-default
	// credentials. A non-empty Endpoint never authenticates.
	Endpoint string
	// TempDir is where Put spools an upload; empty means os.TempDir().
	TempDir string
	// CredentialsFile and CredentialsJSON are explicit credentials that
	// replace application-default credentials. Naming both is refused, and so
	// is naming either with Endpoint.
	CredentialsFile string
	CredentialsJSON []byte
	// ChunkSize bounds the memory one upload request holds. 0 means
	// DefaultChunkSize; otherwise it must be a positive multiple of 256 KiB.
	ChunkSize int64
}

// Store is a GCS storage.Store.
type Store struct {
	bucket    string
	prefix    string
	tempDir   string
	chunkSize int64
	nextKey   func() string
	newToken  func() (string, error)
	// spoolOps are the file operations of the spool; the zero value is the OS.
	spoolOps storagekit.FileOps
	// releaseSpool is how Put releases its spool; a test may wrap it.
	releaseSpool func(*storagekit.SpoolFile) error

	// mu guards client, closed and publishes. It is never held during a build.
	// A client passed to NewFromClient is set at construction and never rebuilt.
	mu     sync.Mutex
	client *gcsstorage.Client
	// build makes the adapter's own client on first use. It is nil for a
	// client the host passed in, which the host owns.
	build func(context.Context) (*gcsstorage.Client, error)
	// publishes counts the adapter-built clients kept, for a test.
	publishes int
	closed    bool
}

// New returns a Store for cfg. It validates the configuration and makes no
// request. The SDK client, and with it the credential lookup, is built on
// first use, so a missing credential or an unreachable endpoint shows there
// rather than here. ctx does not bound that client, which outlives this call.
func New(_ context.Context, cfg Config) (*Store, error) {
	return newStore(cfg)
}

// NewFromClient returns a Store that uses client, which the host built and
// owns. Config's Bucket, Prefix, ChunkSize and TempDir apply; Endpoint and the
// credential fields are refused, since client already carries them.
func NewFromClient(client *gcsstorage.Client, cfg Config) (*Store, error) {
	if cfg.Endpoint != "" || cfg.CredentialsFile != "" || len(cfg.CredentialsJSON) > 0 {
		return nil, errors.New("storage/gcs: NewFromClient refuses Config.Endpoint and the credential fields: the client already carries them")
	}
	prefix, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("storage/gcs: NewFromClient: client is nil")
	}
	s := newBase(cfg, prefix)
	s.client = client
	return s, nil
}

func newStore(cfg Config, extra ...option.ClientOption) (*Store, error) {
	// The lazy build reads the credentials later, so it must not share the
	// caller's slice, which the caller may change after New.
	cfg.CredentialsJSON = bytes.Clone(cfg.CredentialsJSON)
	prefix, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	s := newBase(cfg, prefix)
	s.build = func(ctx context.Context) (*gcsstorage.Client, error) { return dial(ctx, cfg, extra...) }
	return s, nil
}

func newBase(cfg Config, prefix string) *Store {
	chunk := cfg.ChunkSize
	if chunk == 0 {
		chunk = DefaultChunkSize
	}
	return &Store{bucket: cfg.Bucket, prefix: prefix, tempDir: cfg.TempDir, chunkSize: chunk, nextKey: storage.NewKey, newToken: randomToken, releaseSpool: releaseSpoolFile}
}

// dial builds the adapter's client. The SDK reads STORAGE_EMULATOR_HOST
// itself, and only when it is set, so the authorized client is built here and
// given an explicit endpoint: the variable then cannot redirect requests or
// turn authentication off.
func dial(ctx context.Context, cfg Config, extra ...option.ClientOption) (*gcsstorage.Client, error) {
	opts := []option.ClientOption{gcsstorage.WithJSONReads()}
	var raw []byte
	if cfg.Endpoint != "" {
		opts = append(opts, option.WithEndpoint(strings.TrimSuffix(cfg.Endpoint, "/")+"/storage/v1/"), option.WithoutAuthentication())
	} else {
		var err error
		if raw, err = credentialBytes(cfg); err != nil {
			return nil, err
		}
		hc, err := authorizedClient(ctx, raw)
		if err != nil {
			return nil, err
		}
		opts = append(opts, option.WithHTTPClient(hc), option.WithEndpoint(googleEndpoint))
	}
	opts = append(opts, extra...)
	client, err := gcsstorage.NewClient(ctx, opts...)
	if err != nil {
		return nil, credentialError("storage/gcs: client", err, raw)
	}
	return client, nil
}

// credentialBytes returns the explicit credentials document, read from
// CredentialsFile when one is named, or nil when application-default
// credentials apply. A read failure is returned as text: the operating system
// error names the path, never the contents, and carries no chain.
func credentialBytes(cfg Config) ([]byte, error) {
	if cfg.CredentialsFile == "" {
		return cfg.CredentialsJSON, nil
	}
	b, err := os.ReadFile(cfg.CredentialsFile)
	if err != nil {
		return nil, errors.New("storage/gcs: read credentials file: " + err.Error())
	}
	return b, nil
}

// authorizedClient returns an HTTP client that authenticates with the
// credentials document raw, or with application-default credentials when raw
// is nil.
func authorizedClient(ctx context.Context, raw []byte) (*http.Client, error) {
	opts := []option.ClientOption{option.WithScopes(gcsstorage.ScopeFullControl, cloudPlatform)}
	if raw != nil {
		opts = append(opts, option.WithCredentialsJSON(raw))
	}
	hc, _, err := htransport.NewClient(ctx, opts...)
	if err != nil {
		return nil, credentialError("storage/gcs: client", err, raw)
	}
	return hc, nil
}

// credentialError is the error for a failure constructing credentials or the
// SDK client. It carries no wrapped chain, since the SDK error may quote the
// credentials, and it never returns the SDK error's text unredacted.
func credentialError(prefix string, err error, raw []byte) error {
	if raw == nil {
		return errors.New(prefix + ": " + err.Error())
	}
	return redactCredentials(prefix+": "+err.Error(), raw)
}

// redactCredentials returns msg with every string value of the credentials
// document raw removed, in both its decoded form and its JSON-escaped form,
// because the SDK quotes parts of its input in either form. Malformed
// credentials get a fixed message instead, since nothing says which part of
// them the parser would quote.
func redactCredentials(msg string, raw []byte) error {
	var doc json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return errors.New("storage/gcs: client: credentials are not valid JSON")
	}
	leaves, err := stringLeaves(doc, nil)
	if err != nil {
		return errors.New("storage/gcs: client: credentials are not valid JSON")
	}
	// Longest first: replacing a value that sits inside a longer one would
	// leave the rest of the longer one in the message.
	sort.Slice(leaves, func(i, j int) bool { return len(leaves[i]) > len(leaves[j]) })
	for _, leaf := range leaves {
		// Short values would match inside ordinary words; no credential field
		// is that short.
		if len(leaf) < 4 {
			continue
		}
		for _, form := range redactionForms(leaf) {
			msg = strings.ReplaceAll(msg, form, "[redacted]")
		}
	}
	return errors.New(msg)
}

// redactionForms returns the forms of a credential value that may appear in
// an SDK message: the value itself, and its JSON string encoding without the
// surrounding quotes, with and without HTML escaping.
func redactionForms(value string) []string {
	forms := []string{value}
	for _, escapeHTML := range []bool{true, false} {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(escapeHTML)
		if enc.Encode(value) != nil {
			continue
		}
		escaped := strings.TrimSuffix(strings.TrimPrefix(buf.String(), `"`), "\"\n")
		if escaped != value {

			forms = append(forms, escaped)
		}
	}
	return forms
}

// stringLeaves appends every string value in raw, at any depth, to acc. Only
// objects and arrays are descended; numbers, booleans and null carry no secret.
func stringLeaves(raw json.RawMessage, acc []string) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return acc, nil
	}
	var err error
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return append(acc, s), nil
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		for _, child := range fields {
			if acc, err = stringLeaves(child, acc); err != nil {
				return nil, err
			}
		}
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		for _, child := range items {
			if acc, err = stringLeaves(child, acc); err != nil {
				return nil, err
			}
		}
	}
	return acc, nil
}

// validate checks cfg and returns the normalized prefix ("" or "a/b/").
func (cfg Config) validate() (string, error) {
	if cfg.Bucket == "" {
		return "", errors.New("storage/gcs: Config.Bucket is empty")
	}
	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", fmt.Errorf("storage/gcs: Config.Endpoint must be an absolute http(s) URL, got %q", cfg.Endpoint)
		}
	}
	hasFile, hasJSON := cfg.CredentialsFile != "", len(cfg.CredentialsJSON) > 0
	if hasFile && hasJSON {
		return "", errors.New("storage/gcs: Config.CredentialsFile and Config.CredentialsJSON are both set; name one")
	}
	if cfg.Endpoint != "" && (hasFile || hasJSON) {
		return "", errors.New("storage/gcs: Config.Endpoint never authenticates; remove the credential fields")
	}
	if cfg.ChunkSize < 0 || cfg.ChunkSize%chunkQuantum != 0 {
		return "", fmt.Errorf("storage/gcs: Config.ChunkSize %d must be 0 or a positive multiple of %d bytes", cfg.ChunkSize, chunkQuantum)
	}
	prefix, err := storagekit.NormalizePrefix(cfg.Prefix)
	if err != nil {
		return "", fmt.Errorf("storage/gcs: %w", err)
	}
	return prefix, nil
}

// buildResult is the outcome of one build of the adapter's client.
type buildResult struct {
	client *gcsstorage.Client
	err    error
}

// sdk returns the SDK client, building the adapter's own on first use. The build
// runs without the lock, so Close and other operations are never held behind
// credential work. It runs under the operation's values but without its
// cancellation, since the token source keeps the build context for refreshes.
// The operation returns as soon as ctx ends; a build still running then is
// published for later operations. A failed build is not remembered.
func (s *Store) sdk(ctx context.Context) (*gcsstorage.Client, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errStoreClosed
	}
	if s.client != nil {
		c := s.client
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	done := make(chan buildResult, 1)
	go func() {
		c, err := s.build(context.WithoutCancel(ctx))
		done <- s.publish(c, err)
	}()
	select {
	case r := <-done:
		return r.client, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// publish keeps a finished build as the Store's client. If another build was
// published first, or the Store was closed meanwhile, the extra client is
// closed and never used.
func (s *Store) publish(c *gcsstorage.Client, err error) buildResult {
	if err != nil {
		return buildResult{err: err}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = c.Close()
		return buildResult{err: errStoreClosed}
	}
	if s.client != nil {
		_ = c.Close()
		return buildResult{client: s.client}
	}
	s.client = c
	s.publishes++
	return buildResult{client: c}
}

func (s *Store) objectName(key string) string { return s.prefix + key }

// object returns an error only because the SDK client is built lazily, on
// first use; the handle itself needs no request.
func (s *Store) object(ctx context.Context, key string) (*gcsstorage.ObjectHandle, error) {
	client, err := s.sdk(ctx)
	if err != nil {
		return nil, err
	}
	return client.Bucket(s.bucket).Object(s.objectName(key)), nil
}

// Close releases the client the Store built, if it built one, and makes the
// Store refuse further use. A client passed to NewFromClient belongs to the
// host: Close does nothing to it, and the Store stays usable.
//
// Close must not run while an operation is using the client: the SDK clears
// its state on Close, so an operation still using it would fail. A build still
// running when Close is called is closed as soon as it finishes.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.build == nil || s.closed {
		return nil
	}
	s.closed = true
	if s.client == nil {
		return nil
	}
	err := s.client.Close()
	s.client = nil
	if err != nil {
		return fmt.Errorf("storage/gcs: close: %w", err)
	}
	return nil
}

// Stat implements storage.Store. An object is present only when it carries a
// valid SHA-256, which marks it as one this package wrote completely.
func (s *Store) Stat(ctx context.Context, key string) (storage.Info, error) {
	if err := storagekit.Check(ctx, key); err != nil {
		return storage.Info{}, err
	}
	obj, err := s.object(ctx, key)
	if err != nil {
		return storage.Info{}, err
	}
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return storage.Info{}, mapError("stat", err)
	}
	sha := attrs.Metadata[shaMetadata]
	if !storagekit.ValidSHA256(sha) {
		return storage.Info{}, storage.ErrNotFound
	}
	return storage.Info{
		Key:         key,
		Size:        attrs.Size,
		SHA256:      sha,
		ContentType: attrs.ContentType,
		ETag:        storage.ETagFor(sha),
		ModTime:     attrs.Created.UTC(),
	}, nil
}

// Open implements storage.Store.
func (s *Store) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	info, err := s.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	end, err := storagekit.Range(info.Size, offset, length)
	if err != nil {
		return nil, err
	}
	if end == offset {
		return io.NopCloser(strings.NewReader("")), nil
	}
	obj, err := s.object(ctx, key)
	if err != nil {
		return nil, err
	}
	reader, err := obj.NewRangeReader(ctx, offset, end-offset)
	if err != nil {
		return nil, mapError("open", err)
	}
	return reader, nil
}

// Delete implements storage.Store. A missing object is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := storagekit.Check(ctx, key); err != nil {
		return err
	}
	obj, err := s.object(ctx, key)
	if err != nil {
		return err
	}
	if err := obj.Delete(ctx); err != nil && !errors.Is(err, gcsstorage.ErrObjectNotExist) {
		return mapError("delete", err)
	}
	return nil
}

// mapError turns the failures a caller acts on into the storage sentinels,
// and wraps everything else with its operation while keeping the SDK error.
func mapError(operation string, err error) error {
	if errors.Is(err, gcsstorage.ErrObjectNotExist) {
		return storage.ErrNotFound
	}
	var g *googleapi.Error
	if errors.As(err, &g) && g.Code == 416 {
		return storage.ErrInvalidRange
	}
	return fmt.Errorf("storage/gcs: %s: %w", operation, err)
}
