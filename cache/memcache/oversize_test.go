package memcache_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/memcache"
)

func TestARealServerRefusesAnOversizedValueAsErrTooLarge(t *testing.T) {
	addr := os.Getenv("TANGO_TEST_MEMCACHE_ADDR")
	if addr == "" {
		t.Skip("no server")
	}
	s, _ := memcache.New(memcache.Config{Servers: []string{addr}})
	defer s.Close()
	err := s.Set(t.Context(), "probe:big", make([]byte, 2<<20), time.Minute)
	if !errors.Is(err, cache.ErrTooLarge) {
		t.Fatal("the real server's refusal was not mapped to ErrTooLarge")
	}
	if _, ok, err := s.Get(t.Context(), "probe:big"); ok || err != nil {
		t.Fatalf("after a refused Set: ok=%v err=%v", ok, err)
	}
}
