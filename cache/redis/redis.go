// Package redis is a cache.Store on Redis or Valkey, in its own Go module so
// the go-redis client never reaches a project that does not import it.
//
// New connects, verifies the connection with a PING and owns the client it
// creates; Close closes it. NewFromClient wraps a client you already have
// (Cluster, Sentinel, a Unix socket, dynamic credentials): it does not ping
// and does not own the client, so its Close does nothing and you remain
// responsible for closing the client you supplied.
//
// Every command receives the caller's context. A Redis server that refuses
// a value for its size is reported as cache.ErrTooLarge; every other server
// or network error is returned wrapped, never hidden. Support is claimed
// only for the Redis and Valkey versions the module's CI runs.
package redis

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/angvp/tango/cache"
)

// Config describes the server New connects to.
type Config struct {
	// URL is required: redis://[user:password@]host:port[/db] or rediss://
	// for TLS. The database number is the URL path (redis://host:6379/3);
	// there is no separate field for it.
	URL string
	// Username and Password, when not empty, override the URL's userinfo.
	Username string
	Password string
	// TLSConfig customises TLS. It requires a rediss:// URL: a TLSConfig
	// with redis:// is rejected rather than silently changing transport
	// security.
	TLSConfig *tls.Config
	// DialTimeout, ReadTimeout and WriteTimeout, when positive, override the
	// client's defaults.
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	// PoolSize, when positive, is the most connections the client keeps.
	PoolSize int
}

// Store is a Redis- or Valkey-backed cache.Store.
type Store struct {
	client goredis.UniversalClient
	owned  bool
}

var _ cache.Store = (*Store)(nil)

// New connects to the server cfg describes and verifies it with a PING under
// ctx. It fails fast on a bad URL, bad credentials or an unreachable server;
// the returned error is ordinary, so an application may choose to start
// without its cache. If the PING fails, the client New created is closed, so
// a failed New leaks no connection. The Store owns its client: Close closes
// it.
func New(ctx context.Context, cfg Config) (*Store, error) {
	opts, err := options(cfg)
	if err != nil {
		return nil, err
	}
	client := goredis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close() // a failed constructor must not leak connections
		return nil, fmt.Errorf("cache/redis: connect: %w", err)
	}
	return &Store{client: client, owned: true}, nil
}

// NewFromClient wraps a client you built, for Cluster, Sentinel, Unix
// sockets, dynamic credentials and other deployment shapes.
//
// OWNERSHIP: NewFromClient does not ping, and the returned Store does not own
// client. Its Close is intentionally a no-op; you remain responsible for
// closing the client you supplied, when you are done with it.
func NewFromClient(client goredis.UniversalClient) *Store {
	return &Store{client: client}
}

// Close closes the client a Store created with New. For a Store made by
// NewFromClient it does nothing: the host closes its own client.
func (s *Store) Close() error {
	if !s.owned {
		return nil
	}
	return s.client.Close()
}

// options validates cfg and builds the client options from it.
func options(cfg Config) (*goredis.Options, error) {
	if cfg.URL == "" {
		return nil, errors.New("cache/redis: Config.URL is required")
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, errors.New("cache/redis: invalid URL")
	}
	switch u.Scheme {
	case "redis":
		if cfg.TLSConfig != nil {
			return nil, errors.New("cache/redis: a TLSConfig needs a rediss:// URL; refusing to change transport security silently")
		}
	case "rediss":
	default:
		return nil, fmt.Errorf("cache/redis: URL scheme must be redis:// or rediss://, got %q", u.Scheme)
	}
	if cfg.DialTimeout < 0 || cfg.ReadTimeout < 0 || cfg.WriteTimeout < 0 || cfg.PoolSize < 0 {
		return nil, errors.New("cache/redis: timeouts and PoolSize must not be negative")
	}
	opts, err := goredis.ParseURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("cache/redis: invalid URL: %w", err)
	}
	if cfg.TLSConfig != nil {
		opts.TLSConfig = cfg.TLSConfig
	}
	if cfg.Username != "" {
		opts.Username = cfg.Username
	}
	if cfg.Password != "" {
		opts.Password = cfg.Password
	}
	if cfg.DialTimeout > 0 {
		opts.DialTimeout = cfg.DialTimeout
	}
	if cfg.ReadTimeout > 0 {
		opts.ReadTimeout = cfg.ReadTimeout
	}
	if cfg.WriteTimeout > 0 {
		opts.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}
	return opts, nil
}

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := check(ctx, key); err != nil {
		return nil, false, err
	}
	value, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, wrap("get", err)
	}
	return value, true, nil
}

// Set implements cache.Store. The expiry is sent in whole milliseconds,
// rounded up, so the TTL never elapses sooner than asked (the server may still
// evict the value earlier under its memory policy).
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if err := cache.CheckTTL(ttl); err != nil {
		return err
	}
	expiry := ((ttl + time.Millisecond - 1) / time.Millisecond) * time.Millisecond
	if err := s.client.Set(ctx, key, value, expiry).Err(); err != nil {
		return wrap("set", err)
	}
	return nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return wrap("delete", err)
	}
	return nil
}

// check is the preamble every call shares: the context, then the key.
func check(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cache.ValidKey(key) {
		return cache.ErrInvalidKey
	}
	return nil
}

// wrap adds the operation to a server or network error, mapping a server's
// own size refusal to cache.ErrTooLarge.
func wrap(op string, err error) error {
	if tooLarge(err) {
		return fmt.Errorf("cache/redis: %s: %w: %v", op, cache.ErrTooLarge, err)
	}
	return fmt.Errorf("cache/redis: %s: %w", op, err)
}

// tooLarge reports a server refusing a value for its size
// (proto-max-bulk-len, request size limits).
func tooLarge(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "exceeds maximum allowed size") ||
		strings.Contains(msg, "invalid bulk length") ||
		strings.Contains(msg, "max request size exceeded")
}
