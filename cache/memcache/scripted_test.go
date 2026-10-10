package memcache_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/memcache"
)

// scriptedServer speaks just enough of Memcached's text protocol: reply is
// given the command line and, for a set, its data block.
type scriptedServer struct {
	addr string
	mu   sync.Mutex
	seen []string
}

func newScriptedServer(t *testing.T, reply func(line, data string) string) *scriptedServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptedServer{addr: ln.Addr().String()}
	var wg sync.WaitGroup
	var conns sync.Map
	t.Cleanup(func() {
		ln.Close()
		conns.Range(func(c, _ any) bool { c.(net.Conn).Close(); return true })
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conns.Store(conn, true)
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					line = strings.TrimRight(line, "\r\n")
					data := ""
					if strings.HasPrefix(line, "set ") {
						var key string
						var flags, exp, size int
						fmt.Sscanf(line, "set %s %d %d %d", &key, &flags, &exp, &size)
						block := make([]byte, size+2)
						if _, err := io.ReadFull(r, block); err != nil {
							return
						}
						data = string(block[:size])
					}
					s.mu.Lock()
					s.seen = append(s.seen, line)
					s.mu.Unlock()
					io.WriteString(conn, reply(line, data))
				}
			}()
		}
	}()
	return s
}

func (s *scriptedServer) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

func storeFor(t *testing.T, reply func(line, data string) string) (*memcache.Store, *scriptedServer) {
	t.Helper()
	srv := newScriptedServer(t, reply)
	store, err := memcache.New(memcache.Config{Servers: []string{srv.addr}, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, srv
}

func TestGetTellsAHitFromAMissFromAFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storeFor(t, func(line, _ string) string {
		switch line {
		case "gets hit":
			return "VALUE hit 0 3\r\nabc\r\nEND\r\n"
		case "gets miss":
			return "END\r\n"
		}
		return "SERVER_ERROR out of memory\r\n"
	})
	if v, ok, err := store.Get(ctx, "hit"); err != nil || !ok || string(v) != "abc" {
		t.Fatalf("hit = %q, %v, %v", v, ok, err)
	}
	if v, ok, err := store.Get(ctx, "miss"); err != nil || ok || v != nil {
		t.Fatalf("miss = %q, %v, %v; want a miss with no error", v, ok, err)
	}
	_, ok, err := store.Get(ctx, "broken")
	if err == nil || ok || !strings.Contains(err.Error(), "cache/memcache: get") {
		t.Fatalf("failure = %v, ok %v; want an error naming the get", err, ok)
	}
}

func TestSetSendsAWholeSecondExpiryAndClassifiesFailures(t *testing.T) {
	ctx := context.Background()
	store, srv := storeFor(t, func(line, data string) string {
		switch {
		case strings.HasPrefix(line, "set big "):
			return "SERVER_ERROR object too large for cache\r\n"
		case strings.HasPrefix(line, "set sick "):
			return "SERVER_ERROR out of memory storing object\r\n"
		}
		return "STORED\r\n"
	})
	if err := store.Set(ctx, "ok", []byte("v"), 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := srv.lines(); len(got) != 1 || got[0] != "set ok 0 2 1" {
		t.Fatalf("sent %q, want 'set ok 0 2 1' (1.5s rounded up to 2s)", got)
	}
	if err := store.Set(ctx, "big", []byte("v"), time.Minute); !errors.Is(err, cache.ErrTooLarge) {
		t.Errorf("object too large -> %v, want ErrTooLarge", err)
	}
	err := store.Set(ctx, "sick", []byte("v"), time.Minute)
	if err == nil || errors.Is(err, cache.ErrTooLarge) || !strings.Contains(err.Error(), "cache/memcache: set") {
		t.Errorf("other failure = %v, want a plain set failure", err)
	}
}

func TestDeleteTreatsAMissingKeyAsSuccessAndWrapsAFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storeFor(t, func(line, _ string) string {
		switch line {
		case "delete there":
			return "DELETED\r\n"
		case "delete gone":
			return "NOT_FOUND\r\n"
		}
		return "SERVER_ERROR boom\r\n"
	})
	for _, k := range []string{"there", "gone"} {
		if err := store.Delete(ctx, k); err != nil {
			t.Errorf("Delete(%s) = %v, want nil", k, err)
		}
	}
	if err := store.Delete(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "cache/memcache: delete") {
		t.Fatalf("Delete failure = %v", err)
	}
}

func TestSeveralServersAndASmallIdlePoolAreAccepted(t *testing.T) {
	store, err := memcache.New(memcache.Config{Servers: []string{"a:11211", "b:11211"}, MaxIdleConns: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
