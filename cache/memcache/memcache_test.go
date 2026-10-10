package memcache_test

import (
	"crypto/tls"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/memcache"
	"github.com/angvp/tango/cachetest"
)

func TestNewValidatesTheConfig(t *testing.T) {
	for name, cfg := range map[string]memcache.Config{
		"no servers":          {},
		"an empty server":     {Servers: []string{"localhost:11211", " "}},
		"a negative timeout":  {Servers: []string{"localhost:11211"}, Timeout: -time.Second},
		"negative idle conns": {Servers: []string{"localhost:11211"}, MaxIdleConns: -1},
	} {
		t.Run(name, func(t *testing.T) {
			if s, err := memcache.New(cfg); err == nil {
				s.Close()
				t.Fatal("New accepted an invalid configuration")
			}
		})
	}
	s, err := memcache.New(memcache.Config{Servers: []string{"localhost:11211"}})
	if err != nil {
		t.Fatalf("a minimal config: %v", err)
	}
	s.Close()
}

func TestAZeroTimeoutMeansTheDocumentedDefault(t *testing.T) {
	if memcache.DefaultTimeout != 500*time.Millisecond {
		t.Fatalf("DefaultTimeout = %v, want 500ms", memcache.DefaultTimeout)
	}
}

func TestTLSConfigWiresTheClientsDialer(t *testing.T) {
	plain, _ := memcache.New(memcache.Config{Servers: []string{"localhost:11211"}})
	defer plain.Close()
	if plain.HasTLSDialer() {
		t.Error("a Store without TLSConfig has a TLS dialer")
	}
	secure, err := memcache.New(memcache.Config{Servers: []string{"localhost:11211"}, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}})
	if err != nil {
		t.Fatal(err)
	}
	defer secure.Close()
	if !secure.HasTLSDialer() {
		t.Error("TLSConfig did not wire the client's DialContext")
	}
}

func TestATTLIsRoundedUpToWholeSecondsAndNeverToZero(t *testing.T) {
	for name, tt := range map[string]struct {
		ttl  time.Duration
		want int32
	}{
		"one nanosecond":            {1, 1},
		"one millisecond":           {time.Millisecond, 1},
		"just under a second":       {999 * time.Millisecond, 1},
		"exactly a second":          {time.Second, 1},
		"a second and a nanosecond": {time.Second + 1, 2},
		"a minute":                  {time.Minute, 60},
		"1.5 seconds":               {1500 * time.Millisecond, 2},
		"exactly 30 days":           {cache.MaxTTL, 30 * 24 * 3600},
	} {
		t.Run(name, func(t *testing.T) {
			if got := memcache.ExpirySeconds(tt.ttl); got != tt.want {
				t.Fatalf("ExpirySeconds(%v) = %d, want %d", tt.ttl, got, tt.want)
			}
		})
	}
	if memcache.ExpirySeconds(cache.MaxTTL) > 30*24*3600 {
		t.Fatal("30 days must stay a relative expiry (Memcached reads more as a Unix time)")
	}
}

func TestOperationsRefuseBadInputBeforeAnyRequest(t *testing.T) {
	// No server is listening on this port: an error other than the
	// sentinels would mean a request was attempted.
	s, err := memcache.New(memcache.Config{Servers: []string{"127.0.0.1:1"}, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if _, _, err := s.Get(ctx, "bad key"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("Get: %v, want ErrInvalidKey", err)
	}
	if err := s.Set(ctx, "ok", []byte("v"), 0); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Errorf("Set ttl 0: %v, want ErrInvalidTTL", err)
	}
	if err := s.Set(ctx, "ok", []byte("v"), cache.MaxTTL+1); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Errorf("Set ttl > 30 days: %v, want ErrInvalidTTL", err)
	}
	if err := s.Delete(ctx, ""); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("Delete: %v, want ErrInvalidKey", err)
	}
}

func TestAServerErrorIsAnErrorNotAMiss(t *testing.T) {
	s, _ := memcache.New(memcache.Config{Servers: []string{"127.0.0.1:1"}, Timeout: 50 * time.Millisecond})
	defer s.Close()
	if _, ok, err := s.Get(t.Context(), "k"); err == nil || ok {
		t.Fatalf("an unreachable server: ok=%v err=%v, want an error", ok, err)
	}
}

func TestTooLargeClassification(t *testing.T) {
	if !memcache.IsTooLarge(errors.New("memcache: unexpected response line from \"set\": \"SERVER_ERROR object too large for cache\\r\\n\"")) {
		t.Error("the server's too-large refusal was not recognized")
	}
	if memcache.IsTooLarge(errors.New("connection refused")) || memcache.IsTooLarge(nil) {
		t.Error("an unrelated error was classified as too large")
	}
}

// TestMemcachedPassesTheConformanceSuite runs against a real server named by
// TANGO_TEST_MEMCACHE_ADDR (host:port), and is skipped when it is unset.
func TestMemcachedPassesTheConformanceSuite(t *testing.T) {
	addr := os.Getenv("TANGO_TEST_MEMCACHE_ADDR")
	if addr == "" {
		t.Skip("TANGO_TEST_MEMCACHE_ADDR is not set; skipping the real-server conformance run")
	}
	cachetest.Run(t, cachetest.Factory{New: func(t *testing.T) cache.Store {
		s, err := memcache.New(memcache.Config{Servers: []string{addr}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}})
}
