// Package s3 is a storage.Store on Amazon S3 or any S3-compatible object
// store (Cloudflare R2, Backblaze B2, Ceph and others), chosen with Endpoint.
// It lives in its own module so that importing storage or storage/local never
// brings the AWS SDK into a project.
//
// Each object is one S3 object named Prefix+key. Its sniffed content type is
// the object's Content-Type, and its SHA-256 is user metadata, so Stat is one
// HEAD request. A write is create-only: it sends If-None-Match: * and a key
// that already exists is retried with a new one. A provider that ignores
// conditional writes still works, because a generated 160-bit key does not
// collide.
//
// The SHA-256 cannot be known before the stream ends, and S3 fixes an
// object's metadata when the upload starts. So Put first spools the bytes
// into a temporary file (bounded by PutOptions.MaxSize, never in memory),
// then uploads from that file, in parts when it is large.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/angvp/tango/storage"
)

const (
	// DefaultPartSize is the multipart part size when Config.PartSize is 0.
	DefaultPartSize int64 = 8 << 20
	// MinPartSize is the smallest part S3 accepts (the last part may be
	// smaller).
	MinPartSize int64 = 5 << 20
	// maxParts is S3's limit on the parts of one upload.
	maxParts = 10000

	keyAttempts = 3
	shaMetadata = "sha256"

	// abortTimeout bounds the clean-up of a failed multipart upload, which
	// runs even when the request's own context is already done.
	abortTimeout = 30 * time.Second
)

// Credentials are static access keys. Leave Config.Credentials nil to use
// the AWS SDK's standard chain (environment, shared config, instance role).
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// Config describes the bucket a Store keeps its objects in.
type Config struct {
	// Bucket is required.
	Bucket string
	// Region is the bucket's region ("auto" for Cloudflare R2). When empty
	// it comes from the SDK's standard chain.
	Region string
	// Endpoint is the provider's base URL, such as
	// "https://<account>.r2.cloudflarestorage.com". Empty means Amazon S3.
	Endpoint string
	// PathStyle addresses the bucket as Endpoint/bucket/key rather than
	// bucket.Endpoint/key. Most self-hosted and some third-party providers
	// need it.
	PathStyle bool
	// Prefix puts every object under a "folder" of the bucket. It is made of
	// letters, digits, ".", "_" and "-" separated by "/", with no "." or ".."
	// segment.
	Prefix string
	// Credentials are static access keys; nil uses the SDK's standard chain.
	Credentials *Credentials
	// TempDir is where Put spools an upload; empty means os.TempDir().
	TempDir string
	// PartSize is the multipart part size; 0 means DefaultPartSize and
	// anything below MinPartSize is refused.
	PartSize int64
}

// Store is an S3 storage.Store.
type Store struct {
	client   *awss3.Client
	bucket   string
	prefix   string
	tempDir  string
	partSize int64
	nextKey  func() string
}

// New returns a Store for cfg. It validates the configuration but makes no
// request, so a wrong bucket or credential shows up on first use.
func New(ctx context.Context, cfg Config) (*Store, error) {
	prefix, err := cfg.validate()
	if err != nil {
		return nil, err
	}
	var loaders []func(*config.LoadOptions) error
	if cfg.Region != "" {
		loaders = append(loaders, config.WithRegion(cfg.Region))
	}
	if c := cfg.Credentials; c != nil {
		loaders = append(loaders, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken)))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, loaders...)
	if err != nil {
		return nil, fmt.Errorf("storage/s3: load AWS configuration: %w", err)
	}
	if awsCfg.Region == "" {
		return nil, errors.New("storage/s3: no region: set Config.Region or AWS_REGION")
	}
	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.UsePathStyle = cfg.PathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		// Send and check checksums only where S3 requires them. The SDK's
		// newer default trailer checksums are not understood by every
		// S3-compatible provider.
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})
	partSize := cfg.PartSize
	if partSize == 0 {
		partSize = DefaultPartSize
	}
	return &Store{client: client, bucket: cfg.Bucket, prefix: prefix, tempDir: cfg.TempDir, partSize: partSize, nextKey: storage.NewKey}, nil
}

// validate checks cfg and returns the normalized prefix ("" or "a/b/").
func (cfg Config) validate() (string, error) {
	if cfg.Bucket == "" {
		return "", errors.New("storage/s3: Config.Bucket is empty")
	}
	if cfg.Endpoint != "" {
		u, err := url.Parse(cfg.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", fmt.Errorf("storage/s3: Config.Endpoint must be an absolute http(s) URL, got %q", cfg.Endpoint)
		}
	}
	if cfg.PartSize != 0 && cfg.PartSize < MinPartSize {
		return "", fmt.Errorf("storage/s3: Config.PartSize %d is below the %d bytes S3 accepts", cfg.PartSize, MinPartSize)
	}
	return normalizePrefix(cfg.Prefix)
}

// normalizePrefix returns prefix as "" or "a/b/" after checking that it
// cannot name anything outside itself.
func normalizePrefix(prefix string) (string, error) {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return "", nil
	}
	for _, segment := range strings.Split(prefix, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("storage/s3: Config.Prefix %q has an empty, \".\" or \"..\" segment", prefix)
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return "", fmt.Errorf("storage/s3: Config.Prefix %q has the character %q", prefix, c)
			}
		}
	}
	return prefix + "/", nil
}

func (s *Store) objectName(key string) string { return s.prefix + key }

// check is the preamble every call shares: the context, then the key's form.
func check(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !storage.ValidKey(key) {
		return storage.ErrInvalidKey
	}
	return nil
}

// Stat implements storage.Store. An object is present only when it carries
// a valid SHA-256, which marks it as one this package wrote completely.
func (s *Store) Stat(ctx context.Context, key string) (storage.Info, error) {
	if err := check(ctx, key); err != nil {
		return storage.Info{}, err
	}
	out, err := s.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &s.bucket, Key: aws.String(s.objectName(key))})
	if err != nil {
		return storage.Info{}, mapError(err)
	}
	sha := out.Metadata[shaMetadata]
	if !validSHA256(sha) {
		return storage.Info{}, storage.ErrNotFound
	}
	info := storage.Info{Key: key, Size: aws.ToInt64(out.ContentLength), SHA256: sha, ContentType: aws.ToString(out.ContentType), ETag: storage.ETagFor(sha)}
	if out.LastModified != nil {
		info.ModTime = out.LastModified.UTC()
	}
	return info, nil
}

// Open implements storage.Store.
func (s *Store) Open(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	info, err := s.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	if offset < 0 || offset > info.Size {
		return nil, storage.ErrInvalidRange
	}
	end := info.Size
	if length >= 0 && offset+length < info.Size {
		end = offset + length
	}
	if end == offset {
		return io.NopCloser(strings.NewReader("")), nil // S3 refuses an empty range
	}
	out, err := s.client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    aws.String(s.objectName(key)),
		Range:  aws.String(fmt.Sprintf("bytes=%d-%d", offset, end-1)),
	})
	if err != nil {
		return nil, mapError(err)
	}
	return out.Body, nil
}

// Delete implements storage.Store. S3 deletes a missing object without
// complaint, so a missing object is not an error here either.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &s.bucket, Key: aws.String(s.objectName(key))}); err != nil {
		if mapped := mapError(err); !errors.Is(mapped, storage.ErrNotFound) {
			return mapped
		}
	}
	return nil
}

// mapError turns the S3 error codes that mean something to a caller into the
// storage sentinels, and wraps everything else with its context.
func mapError(err error) error {
	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return storage.ErrNotFound
		case "PreconditionFailed", "ConditionalRequestConflict":
			return storage.ErrExists
		case "InvalidRange":
			return storage.ErrInvalidRange
		}
	}
	return fmt.Errorf("storage/s3: %w", err)
}

func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
