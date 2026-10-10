package redis_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/redis"
	"github.com/angvp/tango/cachetest"
)

// TestConformanceAgainstARealServer runs the shared suite against the Redis
// or Valkey server TANGO_TEST_REDIS_URL names, and is skipped when it is
// unset.
func TestConformanceAgainstARealServer(t *testing.T) {
	url := os.Getenv("TANGO_TEST_REDIS_URL")
	if url == "" {
		t.Skip("TANGO_TEST_REDIS_URL is not set")
	}
	cachetest.Run(t, cachetest.Factory{New: func(t *testing.T) cache.Store {
		s, err := redis.New(context.Background(), redis.Config{URL: url})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}})
}

// refusingServer accepts connections, keeps them open and answers every
// read with a Redis error, so a client's PING fails while its connections
// stay in the pool until the client is closed.
type refusingServer struct {
	addr     string
	accepted atomic.Int32
	open     atomic.Int32
	wg       sync.WaitGroup
	mu       sync.Mutex
	conns    []net.Conn
}

// closeAll force-closes every accepted connection, so cleanup cannot hang on
// a client that leaked its connection.
func (s *refusingServer) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

func newRefusingServer(t *testing.T) *refusingServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &refusingServer{addr: ln.Addr().String()}
	t.Cleanup(func() { ln.Close(); s.closeAll(); s.wg.Wait() })
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.conns = append(s.conns, conn)
			s.mu.Unlock()
			s.accepted.Add(1)
			s.open.Add(1)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer conn.Close()
				defer s.open.Add(-1)
				reader := bufio.NewReader(conn)
				for {
					command, err := readCommand(reader)
					if err != nil {
						return // the client closed the connection
					}
					// Let the client's handshake succeed so its connection
					// is healthy and stays pooled; refuse only the PING.
					// That makes an unclosed client hold the connection open.
					switch command {
					case "PING":
						conn.Write([]byte("-ERR refused by the test server\r\n"))
					case "HELLO":
						conn.Write([]byte("-ERR unknown command 'HELLO'\r\n"))
					default:
						conn.Write([]byte("+OK\r\n"))
					}
				}
			}()
		}
	}()
	return s
}

// readCommand reads one RESP array command and returns its upper-cased
// name. It understands just enough of the protocol for the test server.
func readCommand(r *bufio.Reader) (string, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "*")))
	if err != nil {
		return "", err
	}
	name := ""
	for i := 0; i < count; i++ {
		sizeLine, err := r.ReadString('\n')
		if err != nil {
			return "", err
		}
		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(sizeLine, "$")))
		if err != nil {
			return "", err
		}
		arg := make([]byte, size+2) // the bytes and the trailing CRLF
		if _, err := io.ReadFull(r, arg); err != nil {
			return "", err
		}
		if i == 0 {
			name = strings.ToUpper(string(arg[:size]))
		}
	}
	return name, nil
}

func waitNoneOpen(srv *refusingServer, d time.Duration) int32 {
	deadline := time.Now().Add(d)
	for srv.open.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	return srv.open.Load()
}

func TestAFailedNewClosesItsClientAndLeaksNoConnection(t *testing.T) {
	srv := newRefusingServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := redis.New(ctx, redis.Config{URL: "redis://" + srv.addr})
	if err == nil {
		s.Close()
		t.Fatal("New succeeded against a server that refuses everything")
	}
	if srv.accepted.Load() == 0 {
		t.Fatal("the test server never saw a connection; the test proves nothing")
	}
	if open := waitNoneOpen(srv, 3*time.Second); open != 0 {
		t.Fatalf("%d connection(s) still open after New failed: the failed constructor leaked its client", open)
	}
}

func TestNewWithACancelledContextFailsAndLeaksNothing(t *testing.T) {
	srv := newRefusingServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, err := redis.New(ctx, redis.Config{URL: "redis://" + srv.addr})
	if err == nil {
		s.Close()
		t.Fatal("New succeeded with a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if open := waitNoneOpen(srv, 2*time.Second); open != 0 {
		t.Fatalf("%d connection(s) leaked", open)
	}
}

func TestNewFromClientDoesNotPingAndCloseDoesNotCloseTheClient(t *testing.T) {
	srv := newRefusingServer(t)
	client := goredis.NewClient(&goredis.Options{Addr: srv.addr, MaxRetries: -1})
	defer client.Close()

	store := redis.NewFromClient(client) // would connect if it pinged
	if srv.accepted.Load() != 0 {
		t.Fatal("NewFromClient connected to the server; it must not ping")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close = %v, want a no-op", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close = %v, want a no-op", err)
	}
	if err := client.Ping(context.Background()).Err(); errors.Is(err, goredis.ErrClosed) {
		t.Fatal("Store.Close closed a client it does not own")
	}
}

func TestInvalidKeysTTLsAndContextsAreRefusedBeforeAnyRequest(t *testing.T) {
	srv := newRefusingServer(t)
	client := goredis.NewClient(&goredis.Options{Addr: srv.addr, MaxRetries: -1})
	defer client.Close()
	store := redis.NewFromClient(client)
	ctx := context.Background()
	if _, _, err := store.Get(ctx, "bad key"); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("Get: %v, want ErrInvalidKey", err)
	}
	if err := store.Set(ctx, "k", []byte("v"), 0); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Errorf("Set ttl 0: %v, want ErrInvalidTTL", err)
	}
	if err := store.Set(ctx, "k", []byte("v"), cache.MaxTTL+1); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Errorf("Set ttl > max: %v, want ErrInvalidTTL", err)
	}
	if err := store.Delete(ctx, ""); !errors.Is(err, cache.ErrInvalidKey) {
		t.Errorf("Delete: %v, want ErrInvalidKey", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Set(cancelled, "k", []byte("v"), time.Minute); !errors.Is(err, context.Canceled) {
		t.Errorf("Set cancelled: %v, want context.Canceled", err)
	}
	if got := srv.accepted.Load(); got != 0 {
		t.Errorf("the server saw %d connection(s); refusals must happen before any request", got)
	}
}
