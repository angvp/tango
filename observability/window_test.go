package observability_test

import (
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/observability"
)

var windowStart = time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)

// fakeClock is a settable clock for Window tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// observeRequest feeds a Window the same attributes the access logger passes.
func observeRequest(w *observability.Window, seconds float64, status int) {
	w.ObserveHistogram(observability.MetricHTTPRequestDuration, seconds,
		slog.String("route", "/items/{id}/"), slog.String("method", "GET"), slog.Int("status", status))
}

// recordJob feeds a Window the same attributes the job loop passes.
func recordJob(w *observability.Window, outcome string) {
	w.AddCounter(observability.MetricSchedulerJobInvocations, 1,
		slog.String("job", "cleanup"), slog.String("outcome", outcome))
}

func mustSnapshot(t *testing.T, w *observability.Window) observability.Snapshot {
	t.Helper()
	snap, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	return snap
}

func newClockedWindow(clock *fakeClock) *observability.Window {
	return observability.NewWindow(observability.WithClock(clock.Now))
}

func TestWindowEmptySnapshotReportsZeroValues(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	snap := mustSnapshot(t, newClockedWindow(clock))

	if snap.Scope != "this instance only" {
		t.Errorf("Scope = %q, want %q", snap.Scope, "this instance only")
	}
	if !snap.Since.Equal(windowStart) {
		t.Errorf("Since = %v, want %v", snap.Since, windowStart)
	}
	if snap.Window != 60*time.Minute {
		t.Errorf("Window = %v, want 60m", snap.Window)
	}
	if snap.Requests != (observability.RequestSummary{}) {
		t.Errorf("Requests = %+v, want zero value", snap.Requests)
	}
	if snap.Jobs != (observability.JobSummary{}) {
		t.Errorf("Jobs = %+v, want zero value", snap.Jobs)
	}
}

func TestNewWindowWithoutOptionsUsesWallClock(t *testing.T) {
	before := time.Now()
	snap := mustSnapshot(t, observability.NewWindow())
	if snap.Since.Before(before) || snap.Since.After(time.Now()) {
		t.Errorf("Since = %v, want a wall-clock time between %v and now", snap.Since, before)
	}
}

func TestWindowSinceIsWhenTheWindowStarted(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)

	clock.Set(windowStart.Add(20 * time.Minute))
	observeRequest(w, 0.01, 200)

	if snap := mustSnapshot(t, w); !snap.Since.Equal(windowStart) {
		t.Errorf("Since = %v, want window start %v", snap.Since, windowStart)
	}
}

func TestWindowCountsRequestsByStatusClass(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)
	for _, status := range []int{200, 201, 304, 404, 500, 503} {
		observeRequest(w, 0.05, status)
	}

	got := mustSnapshot(t, w).Requests
	want := observability.RequestSummary{Total: 6, Class2xx: 2, Class3xx: 1, Class4xx: 1, Class5xx: 2,
		LatencyP50: 0.05, LatencyP95: 0.05, LatencyP99: 0.05}
	if got != want {
		t.Errorf("Requests = %+v, want %+v", got, want)
	}
}

func TestWindowIgnoresStatusesOutsideTheFiveClasses(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)
	for _, status := range []int{0, 99, 100, 600} {
		observeRequest(w, 0.05, status)
	}

	if got := mustSnapshot(t, w).Requests; got != (observability.RequestSummary{}) {
		t.Errorf("Requests = %+v, want zero value", got)
	}
}

func TestWindowPercentilesUseFixedBuckets(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)
	for range 90 {
		observeRequest(w, 0.05, 200)
	}
	for range 10 {
		observeRequest(w, 1.0, 500)
	}

	got := mustSnapshot(t, w).Requests
	if got.Total != 100 {
		t.Fatalf("Total = %d, want 100", got.Total)
	}
	if got.LatencyP50 != 0.05 || got.LatencyP95 != 1 || got.LatencyP99 != 1 {
		t.Errorf("percentiles = p50 %v p95 %v p99 %v, want 0.05 1 1",
			got.LatencyP50, got.LatencyP95, got.LatencyP99)
	}
}

func TestWindowReportsPercentileAboveLargestBucketAsInfinity(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)
	observeRequest(w, 120, 200)

	got := mustSnapshot(t, w).Requests
	if !math.IsInf(got.LatencyP99, 1) {
		t.Errorf("LatencyP99 = %v, want +Inf for a value above the largest bucket", got.LatencyP99)
	}
}

func TestWindowRollsOverToTheNextMinute(t *testing.T) {
	clock := &fakeClock{now: windowStart.Add(30 * time.Second)}
	w := newClockedWindow(clock)
	observeRequest(w, 0.01, 200)

	clock.Set(windowStart.Add(70 * time.Second))
	observeRequest(w, 0.01, 500)

	got := mustSnapshot(t, w).Requests
	if got.Total != 2 || got.Class2xx != 1 || got.Class5xx != 1 {
		t.Errorf("Requests = %+v, want Total 2 with one 2xx and one 5xx", got)
	}
}

func TestWindowExpiresActivityAfterSixtyMinutes(t *testing.T) {
	clock := &fakeClock{now: windowStart.Add(30 * time.Second)}
	w := newClockedWindow(clock)
	observeRequest(w, 0.01, 200)

	clock.Set(windowStart.Add(30 * time.Minute))
	observeRequest(w, 0.01, 404)

	clock.Set(windowStart.Add(59*time.Minute + 59*time.Second))
	if got := mustSnapshot(t, w).Requests; got.Total != 2 {
		t.Fatalf("Total at 12:59:59 = %d, want 2 (both still inside the window)", got.Total)
	}

	clock.Set(windowStart.Add(60 * time.Minute))
	got := mustSnapshot(t, w).Requests
	if got.Total != 1 || got.Class2xx != 0 || got.Class4xx != 1 {
		t.Errorf("Requests at 13:00:00 = %+v, want only the 404 left", got)
	}

	clock.Set(windowStart.Add(90 * time.Minute))
	if got := mustSnapshot(t, w).Requests; got != (observability.RequestSummary{}) {
		t.Errorf("Requests at 13:30:00 = %+v, want zero value after all expiry", got)
	}
}

func TestWindowReusedBucketDoesNotKeepExpiredCounts(t *testing.T) {
	clock := &fakeClock{now: windowStart.Add(30 * time.Second)}
	w := newClockedWindow(clock)
	observeRequest(w, 0.01, 200)

	// Exactly one hour later the 12:00 slot is reused for 13:00.
	clock.Set(windowStart.Add(60*time.Minute + 30*time.Second))
	observeRequest(w, 0.01, 503)

	got := mustSnapshot(t, w).Requests
	if got.Total != 1 || got.Class2xx != 0 || got.Class5xx != 1 {
		t.Errorf("Requests = %+v, want only the 503 in the reused slot", got)
	}
}

func TestWindowCountsJobOutcomes(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)
	for _, outcome := range []string{"success", "success", "error", "panic", "canceled"} {
		recordJob(w, outcome)
	}

	got := mustSnapshot(t, w).Jobs
	want := observability.JobSummary{Success: 2, Error: 1, Panic: 1, Canceled: 1}
	if got != want {
		t.Errorf("Jobs = %+v, want %+v", got, want)
	}
}

func TestWindowIgnoresUnknownMetricsAndOutcomes(t *testing.T) {
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)

	w.AddCounter(observability.MetricRealtimeRoomEvents, 1, slog.String("outcome", "success"))
	w.ObserveHistogram(observability.MetricRealtimeRoomEvents, 0.1, slog.Int("status", 200))
	w.AddCounter("some_other_counter", 1, slog.String("outcome", "success"))
	w.ObserveHistogram("some_other_histogram", 0.1, slog.Int("status", 500))

	recordJob(w, "bogus")
	recordJob(w, "")
	w.AddCounter(observability.MetricSchedulerJobInvocations, 1)
	w.ObserveHistogram(observability.MetricHTTPRequestDuration, 0.1)
	w.ObserveHistogram(observability.MetricHTTPRequestDuration, 0.1, slog.String("status", "200"))
	w.AddCounter(observability.MetricSchedulerJobInvocations, 0, slog.String("outcome", "success"))

	snap := mustSnapshot(t, w)
	if snap.Requests != (observability.RequestSummary{}) {
		t.Errorf("Requests = %+v, want zero value", snap.Requests)
	}
	if snap.Jobs != (observability.JobSummary{}) {
		t.Errorf("Jobs = %+v, want zero value", snap.Jobs)
	}
}

func TestWindowRecordsAndSnapshotsConcurrently(t *testing.T) {
	const workers, perWorker = 8, 500
	clock := &fakeClock{now: windowStart}
	w := newClockedWindow(clock)

	stop := make(chan struct{})
	var snapshots sync.WaitGroup
	snapshots.Add(1)
	go func() {
		defer snapshots.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := w.Snapshot(); err != nil {
				t.Errorf("Snapshot() error = %v, want nil", err)
				return
			}
		}
	}()

	var writers sync.WaitGroup
	for range workers {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range perWorker {
				observeRequest(w, 0.02, 200)
				recordJob(w, "success")
			}
		}()
	}
	writers.Wait()
	close(stop)
	snapshots.Wait()

	snap := mustSnapshot(t, w)
	if snap.Requests.Total != workers*perWorker || snap.Requests.Class2xx != workers*perWorker {
		t.Errorf("Requests = %+v, want %d 2xx requests", snap.Requests, workers*perWorker)
	}
	if snap.Jobs.Success != workers*perWorker {
		t.Errorf("Jobs.Success = %d, want %d", snap.Jobs.Success, workers*perWorker)
	}
}
