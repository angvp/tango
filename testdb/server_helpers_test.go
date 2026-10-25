package testdb

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"
)

// recordingTB is a fakeTB that also records Cleanup registrations and Errorf
// calls. A test runs a helper, then runs the recorded cleanups itself in the
// order testing would, and inspects the failures they report.
type recordingTB struct {
	*fakeTB
	cleanups []func()
	errors   []string
}

func (r *recordingTB) Cleanup(fn func()) {
	r.cleanups = append(r.cleanups, fn)
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

// runCleanups runs the recorded cleanups last-in first-out, as testing does.
func (r *recordingTB) runCleanups() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	r.cleanups = nil
}

// runRecording calls fn with a recordingTB on its own goroutine and waits for
// it, whether fn returns or the fake stops it with Fatalf.
func runRecording(t *testing.T, fn func(tb *recordingTB)) *recordingTB {
	t.Helper()
	rec := &recordingTB{fakeTB: &fakeTB{TB: t}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(rec)
	}()
	<-done
	return rec
}

// randomHex returns n random bytes encoded as hex. It is used for role and
// password material generated inside a test; the values are never printed.
func randomHex(t testing.TB, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(buf)
}
