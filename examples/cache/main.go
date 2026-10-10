// Command cache shows what the cache package is for: speeding up a read that
// is expensive to compute and safe to compute again. GET /rates/EUR/ computes
// an exchange rate once and serves it from a bounded in-process cache for 30
// seconds, reporting X-Cache: hit or miss.
//
// It caches recomputable reads only. It is not ORM or response caching, and
// nothing here invalidates the cache when data changes: every value has a
// TTL, a stale answer is bounded by it, and bulk invalidation is a new key
// prefix (set CACHE_VERSION=2 and restart).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/cache"
	"github.com/angvp/tango/cache/local"
	"github.com/angvp/tango/db"
)

func main() { os.Exit(run()) }

func run() int {
	check := flag.Bool("check", false, "validate app registration and exit")
	flag.Parse()

	store, err := newStore(os.Getenv("CACHE_VERSION"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	config := appConfig(store, &calculator{now: time.Now})

	if *check {
		if err := tango.Check(config); err != nil {
			fmt.Fprintln(os.Stderr, "check failed:", err)
			return 1
		}
		fmt.Println("check passed")
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	if err := tango.ServeContext(ctx, config, nil, db.SQLite); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// newStore is the cache this process uses: bounded in memory, namespaced by a
// version so that bumping it makes every older entry unreachable.
func newStore(version string) (cache.Store, error) {
	if version == "" {
		version = "1"
	}
	base, err := local.New(local.Config{MaxEntries: 1000, MaxBytes: 1 << 20})
	if err != nil {
		return nil, err
	}
	return cache.Prefix(base, "rates:v"+version+":"), nil
}

func appConfig(store cache.Store, calc *calculator) tango.Config {
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{ratesApp(store, calc)}
	return config
}
