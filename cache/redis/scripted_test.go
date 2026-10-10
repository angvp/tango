package redis_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/redis"
)

// scriptedServer answers each command with whatever reply returns, and
// records the commands it saw. It speaks just enough RESP for the adapter.
type scriptedServer struct {
	addr  string
	reply func(args []string) string
	mu    sync.Mutex
	seen  [][]string
}

func newScriptedServer(t *testing.T, reply func(args []string) string) *scriptedServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptedServer{addr: ln.Addr().String(), reply: reply}
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
					args, err := readArgs(r)
					if err != nil {
						return
					}
					s.mu.Lock()
					s.seen = append(s.seen, args)
					s.mu.Unlock()
					switch strings.ToUpper(args[0]) {
					case "HELLO":
						io.WriteString(conn, "-ERR unknown command 'HELLO'\r\n")
					case "PING":
						io.WriteString(conn, "+PONG\r\n")
					case "CLIENT", "SELECT":
						io.WriteString(conn, "+OK\r\n")
					default:
						io.WriteString(conn, s.reply(args))
					}
				}
			}()
		}
	}()
	return s
}

// last returns the most recent command whose name is name.
func (s *scriptedServer) last(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.seen) - 1; i >= 0; i-- {
		if strings.EqualFold(s.seen[i][0], name) {
			return s.seen[i]
		}
	}
	return nil
}

func readArgs(r *bufio.Reader) ([]string, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(header, "*")))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		sizeLine, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(sizeLine, "$")))
		if err != nil {
			return nil, err
		}
		arg := make([]byte, size+2)
		if _, err := io.ReadFull(r, arg); err != nil {
			return nil, err
		}
		args = append(args, string(arg[:size]))
	}
	return args, nil
}

func storeFor(t *testing.T, reply func([]string) string) (*redis.Store, *scriptedServer) {
	t.Helper()
	srv := newScriptedServer(t, reply)
	client := goredis.NewClient(&goredis.Options{Addr: srv.addr, MaxRetries: -1, ReadTimeout: 2 * time.Second})
	t.Cleanup(func() { client.Close() })
	return redis.NewFromClient(client), srv
}

func TestGetTellsAHitFromAMissFromAFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storeFor(t, func(args []string) string {
		switch args[1] {
		case "hit":
			return "$3\r\nabc\r\n"
		case "miss":
			return "$-1\r\n"
		}
		return "-ERR the server is unhappy\r\n"
	})
	if v, ok, err := store.Get(ctx, "hit"); err != nil || !ok || string(v) != "abc" {
		t.Fatalf("hit = %q, %v, %v", v, ok, err)
	}
	if v, ok, err := store.Get(ctx, "miss"); err != nil || ok || v != nil {
		t.Fatalf("miss = %q, %v, %v; want a miss with no error", v, ok, err)
	}
	_, ok, err := store.Get(ctx, "broken")
	if err == nil || ok || errors.Is(err, cache.ErrTooLarge) || !strings.Contains(err.Error(), "cache/redis: get") {
		t.Fatalf("failure = %v, ok %v; want an error naming the get", err, ok)
	}
}

func TestSetSendsAMillisecondExpiryRoundedUp(t *testing.T) {
	store, srv := storeFor(t, func([]string) string { return "+OK\r\n" })
	if err := store.Set(context.Background(), "k", []byte("v"), 1500*time.Microsecond); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(srv.last("SET"), " ")
	if got != "set k v px 2" && got != "SET k v PX 2" {
		t.Fatalf("command = %q, want the expiry sent as PX 2 (1.5ms rounded up)", got)
	}
}

func TestAServersSizeRefusalIsErrTooLargeAndOtherFailuresAreNot(t *testing.T) {
	ctx := context.Background()
	for _, refusal := range []string{
		"-ERR Protocol error: invalid bulk length",
		"-ERR Protocol error: Bulk request exceeds maximum allowed size",
		"-ERR max request size exceeded",
	} {
		store, _ := storeFor(t, func([]string) string { return refusal + "\r\n" })
		if err := store.Set(ctx, "k", []byte("v"), time.Minute); !errors.Is(err, cache.ErrTooLarge) {
			t.Errorf("%q -> %v, want ErrTooLarge", refusal, err)
		}
	}
	store, _ := storeFor(t, func([]string) string { return "-ERR OOM command not allowed\r\n" })
	err := store.Set(ctx, "k", []byte("v"), time.Minute)
	if err == nil || errors.Is(err, cache.ErrTooLarge) || !strings.Contains(err.Error(), "cache/redis: set") {
		t.Fatalf("OOM = %v, want a plain set failure", err)
	}
}

func TestDeleteSucceedsForAnyCountAndWrapsAFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storeFor(t, func(args []string) string {
		if args[1] == "gone" {
			return ":0\r\n"
		}
		if args[1] == "there" {
			return ":1\r\n"
		}
		return "-ERR nope\r\n"
	})
	for _, k := range []string{"gone", "there"} {
		if err := store.Delete(ctx, k); err != nil {
			t.Errorf("Delete(%s) = %v, want nil (a missing key is not an error)", k, err)
		}
	}
	if err := store.Delete(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "cache/redis: delete") {
		t.Fatalf("Delete failure = %v", err)
	}
}

func TestANewStoreOwnsItsClientAndCloseEndsIt(t *testing.T) {
	srv := newScriptedServer(t, func([]string) string { return "+OK\r\n" })
	store, err := redis.New(context.Background(), redis.Config{URL: "redis://" + srv.addr + "/0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := store.Set(context.Background(), "k", []byte("v"), time.Minute); !errors.Is(err, goredis.ErrClosed) {
		t.Fatalf("Set after Close = %v, want the closed-client error", err)
	}
}
