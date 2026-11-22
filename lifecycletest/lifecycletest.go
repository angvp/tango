// Package lifecycletest runs a registry's registered lifecycles inside a test,
// the way ServeContext does before it listens, so a host can test a startup
// seed or warm-up against a test database without serving.
package lifecycletest

import (
	"context"
	"fmt"
	"time"

	"github.com/angvp/tango"
)

// stopTimeout bounds the Stop hooks that run when a test finishes.
const stopTimeout = 10 * time.Second

// T is the part of testing.TB that Start uses.
type T interface {
	Helper()
	Cleanup(func())
	Fatalf(format string, args ...any)
}

// Start runs the Start hook of each Lifecycle registered with registry, in
// registration order, and stops at the first error: it stops the lifecycles
// that already started, in reverse, and fails the test naming the lifecycle.
// When the test finishes it cancels the context the hooks were given and runs
// the Stop hooks of the started lifecycles in reverse. A Lifecycle with no
// Start counts as started so its Stop still runs.
//
// It runs only the lifecycles the host registered: ServeContext's own
// scheduler lifecycle is never started. Call it after the registry's
// RunRegistration and SetStore.
func Start(t T, registry *tango.Registry) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var started []tango.Lifecycle
	stopAll := func() []error {
		cancel()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), stopTimeout)
		defer stopCancel()
		return stopInReverse(stopCtx, started)
	}
	for _, lc := range registry.Lifecycles() {
		if lc.Start != nil {
			if err := lc.Start(ctx); err != nil {
				errs := stopAll()
				t.Fatalf("lifecycletest: start lifecycle %q: %v%s", lc.Name, err, joinStopErrors(errs))
				return
			}
		}
		started = append(started, lc)
	}
	t.Cleanup(func() {
		for _, err := range stopAll() {
			t.Fatalf("lifecycletest: %v", err)
		}
	})
}

func stopInReverse(ctx context.Context, started []tango.Lifecycle) []error {
	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		if started[i].Stop == nil {
			continue
		}
		if err := started[i].Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop lifecycle %q: %w", started[i].Name, err))
		}
	}
	return errs
}

func joinStopErrors(errs []error) string {
	out := ""
	for _, err := range errs {
		out += "; " + err.Error()
	}
	return out
}
