//go:build unix

package cli

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestWatchStopSignalsReportsAnInterrupt(t *testing.T) {
	stop := watchStopSignals()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // signals are delivered asynchronously
	if !stop() {
		t.Fatal("the watcher missed an interrupt")
	}
}

func TestWatchStopSignalsReportsNothingWhenNothingArrived(t *testing.T) {
	if stop := watchStopSignals(); stop() {
		t.Fatal("the watcher reported a signal that never came")
	}
}
