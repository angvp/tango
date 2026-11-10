// Package testdbhook lets a test wrap the driver connector testdb opens for it,
// so an internal helper can observe every statement the test's database sends.
// An attachment is scoped to one testing.TB and ends when that test ends, so
// tests that do not attach run exactly as before.
package testdbhook

import (
	"database/sql/driver"
	"sync"
	"testing"
)

// Wrapper returns the connector to open in place of inner.
type Wrapper func(inner driver.Connector) driver.Connector

var (
	mu       sync.Mutex
	wrappers = map[testing.TB]Wrapper{}
)

// Attach routes the database testdb opens for t through wrap until t ends.
func Attach(t testing.TB, wrap Wrapper) {
	t.Helper()
	mu.Lock()
	wrappers[t] = wrap
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(wrappers, t)
		mu.Unlock()
	})
}

// Wrap returns inner, wrapped by the wrapper attached to t, or inner unchanged
// when none is attached.
func Wrap(t testing.TB, inner driver.Connector) driver.Connector {
	mu.Lock()
	wrap := wrappers[t]
	mu.Unlock()
	if wrap == nil {
		return inner
	}
	return wrap(inner)
}
