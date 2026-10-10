// Package memcache is a cache.Store on Memcached, in its own Go module so no
// client library reaches a project that does not import it.
//
// Memcached is simpler than Redis, and the differences are real, so they are
// stated here instead of hidden behind a symmetric Config:
//
//   - No authentication. The client has no SASL, so Config has no username or
//     password. Use network isolation, or TLS (below), as the security model.
//   - TLS only through the client's dialer, for a server started with
//     --enable-ssl. Config.TLSConfig builds that dialer; nothing else is
//     configurable.
//   - No in-flight cancellation. The client's calls take no context. The
//     adapter checks the context before an operation begins, but once I/O has
//     started Config.Timeout (default 500ms) is the bound.
//   - Several servers are sharded by key with no replication. Changing the
//     server list remaps keys, which shows up as misses: acceptable for a
//     cache, and why one cache must not hold anything you cannot recompute.
//   - Expiry has one-second granularity and zero means "never expires", so a
//     TTL is rounded up to whole seconds and is never turned into zero.
//
// A Store passes the cachetest conformance suite against a real server. See
// https://memcached.org for the server.
package memcache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/angvp/tango/cache"
)

// DefaultTimeout is the socket read/write timeout used when Config.Timeout is
// zero.
const DefaultTimeout = 500 * time.Millisecond

// Config describes the Memcached servers a Store talks to.
type Config struct {
	// Servers lists host:port addresses. It must not be empty. With more than
	// one, keys are sharded across them with no replication.
	Servers []string
	// Timeout bounds each socket read and write, and so every operation once
	// it has begun. Zero means DefaultTimeout; negative is invalid.
	Timeout time.Duration
	// MaxIdleConns is the most idle connections kept per server. Zero uses
	// the client's default; negative is invalid.
	MaxIdleConns int
	// TLSConfig, when set, makes every connection use TLS (the server must be
	// started with --enable-ssl). It is applied through the client's dialer.
	TLSConfig *tls.Config
}

// Store is a Memcached cache.Store. Close it when done.
type Store struct {
	client *memcache.Client
}

var _ cache.Store = (*Store)(nil)

// New validates cfg and returns a Store. It does not connect: the client
// connects lazily and has no handshake to verify.
func New(cfg Config) (*Store, error) {
	if len(cfg.Servers) == 0 {
		return nil, errors.New("cache/memcache: Servers must not be empty")
	}
	for _, server := range cfg.Servers {
		if strings.TrimSpace(server) == "" {
			return nil, errors.New("cache/memcache: Servers contains an empty address")
		}
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("cache/memcache: Timeout must be positive, got %v", cfg.Timeout)
	}
	if cfg.MaxIdleConns < 0 {
		return nil, fmt.Errorf("cache/memcache: MaxIdleConns must not be negative, got %d", cfg.MaxIdleConns)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	client := memcache.New(cfg.Servers...)
	client.Timeout = timeout
	if cfg.MaxIdleConns > 0 {
		client.MaxIdleConns = cfg.MaxIdleConns
	}
	if cfg.TLSConfig != nil {
		client.DialContext = tlsDialer(cfg.TLSConfig, timeout)
	}
	return &Store{client: client}, nil
}

// tlsDialer returns the client's DialContext for a TLS server. tls.Dialer
// takes the server name from the address when the config has none.
func tlsDialer(cfg *tls.Config, timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	dialer := &tls.Dialer{NetDialer: &net.Dialer{Timeout: timeout}, Config: cfg.Clone()}
	return dialer.DialContext
}

// Close closes the idle connections. The Store can still be used afterwards.
func (s *Store) Close() error { return s.client.Close() }

// Get implements cache.Store.
func (s *Store) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if err := check(ctx, key); err != nil {
		return nil, false, err
	}
	item, err := s.client.Get(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("cache/memcache: get: %w", err)
	}
	return item.Value, true, nil
}

// Set implements cache.Store. The TTL is rounded up to whole seconds, at
// least one, so a positive TTL never becomes Memcached's "no expiry".
func (s *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if err := cache.CheckTTL(ttl); err != nil {
		return err
	}
	err := s.client.Set(&memcache.Item{Key: key, Value: value, Expiration: expirySeconds(ttl)})
	if err != nil {
		return setError(err)
	}
	return nil
}

// Delete implements cache.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := check(ctx, key); err != nil {
		return err
	}
	if err := s.client.Delete(key); err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		return fmt.Errorf("cache/memcache: delete: %w", err)
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

// expirySeconds converts a valid TTL (0 < ttl <= 30 days) to Memcached's
// relative expiry: whole seconds rounded up, never below one. 30 days is the
// largest relative expiry Memcached reads as a duration, so the absolute
// timestamp form is never used.
func expirySeconds(ttl time.Duration) int32 {
	seconds := (ttl + time.Second - 1) / time.Second
	if seconds < 1 {
		seconds = 1
	}
	return int32(seconds)
}

// setError reports a failed Set; a server refusing the value for its size is
// cache.ErrTooLarge.
func setError(err error) error {
	if isTooLarge(err) {
		return fmt.Errorf("%w: %v", cache.ErrTooLarge, err)
	}
	return fmt.Errorf("cache/memcache: set: %w", err)
}

// isTooLarge reports whether err is the server's "object too large"
// refusal.
func isTooLarge(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "too large")
}
