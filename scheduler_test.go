package tango

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/db"
)

func noopJob(name string) Job {
	return Job{Name: name, Interval: time.Minute, Run: func(context.Context) error { return nil }}
}

func TestRegisterLifecycleRejectsReservedSchedulerName(t *testing.T) {
	r := NewRegistry()
	err := r.RegisterLifecycle(Lifecycle{Name: schedulerLifecycleName, Stop: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("expected an error for the reserved scheduler name, got nil")
	}
	if !errors.Is(err, ErrReservedLifecycleName) {
		t.Fatalf("err = %v, want errors.Is match against ErrReservedLifecycleName", err)
	}
	if len(r.Lifecycles()) != 0 {
		t.Fatal("a rejected registration must not be recorded")
	}
}

func TestSchedulerLifecyclesOmitsSchedulerWhenNoJobsRegistered(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterLifecycle(Lifecycle{Name: "a", Stop: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("RegisterLifecycle: %v", err)
	}

	got := schedulerLifecycles(r)
	if len(got) != 1 {
		t.Fatalf("schedulerLifecycles() = %d entries, want 1 (no scheduler appended)", len(got))
	}
	if got[0].Name != "a" {
		t.Fatalf("schedulerLifecycles()[0].Name = %q, want %q", got[0].Name, "a")
	}
}

func TestSchedulerLifecyclesAppendsSchedulerLastWhenJobsRegistered(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterLifecycle(Lifecycle{Name: "a", Stop: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("RegisterLifecycle: %v", err)
	}
	if err := r.RegisterLifecycle(Lifecycle{Name: "b", Stop: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("RegisterLifecycle: %v", err)
	}
	if err := r.RegisterJob(noopJob("job-1")); err != nil {
		t.Fatalf("RegisterJob: %v", err)
	}

	got := schedulerLifecycles(r)
	want := []string{"a", "b", schedulerLifecycleName}
	if len(got) != len(want) {
		t.Fatalf("schedulerLifecycles() = %d entries, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("schedulerLifecycles()[%d].Name = %q, want %q", i, got[i].Name, name)
		}
	}
}

func TestSchedulerLifecyclesNeverMutatesRegistryLifecycles(t *testing.T) {
	r := NewRegistry()
	if err := r.RegisterLifecycle(Lifecycle{Name: "a", Stop: func(context.Context) error { return nil }}); err != nil {
		t.Fatalf("RegisterLifecycle: %v", err)
	}
	if err := r.RegisterJob(noopJob("job-1")); err != nil {
		t.Fatalf("RegisterJob: %v", err)
	}

	_ = schedulerLifecycles(r)

	got := r.Lifecycles()
	if len(got) != 1 {
		t.Fatalf("Registry.Lifecycles() = %d entries, want 1 (must never include the scheduler)", len(got))
	}
	if got[0].Name == schedulerLifecycleName {
		t.Fatal("Registry.Lifecycles() must never return the Scheduler lifecycle")
	}
}

func TestSchedulerLifecyclesWithNoLifecyclesAndNoJobsIsEmpty(t *testing.T) {
	r := NewRegistry()
	got := schedulerLifecycles(r)
	if len(got) != 0 {
		t.Fatalf("schedulerLifecycles() = %d entries, want 0", len(got))
	}
}

// TestServeContextRunsSchedulerLifecycleLastToStartFirstToStop exercises the
// wiring end to end: a Job registered alongside a host Lifecycle causes the
// Scheduler lifecycle to be appended after it, so by the ordinary
// reverse-order shutdown rule the Scheduler starts last and stops first.
// The registered Job here never ticks (its ticker is real and its Interval
// is a full minute), so only the host Lifecycle's start/stop order is
// directly observable within the test's short lifetime; a scheduler-side
// ordering bug would surface as ServeContext failing to start or stop
// cleanly. Job loop execution itself is covered by job_loop_test.go and the
// scheduler-level tests below.
func TestServeContextRunsSchedulerLifecycleLastToStartFirstToStop(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	registerAll := NewApp("lifecycle-and-job-owner", func(r *Registry) error {
		if err := r.RegisterLifecycle(orderedComponent("a", &log, &mu, nil, nil)); err != nil {
			return err
		}
		return r.RegisterJob(noopJob("job-1"))
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext error = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:a", "stop:a"}
	if len(log) != len(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	for i, entry := range want {
		if log[i] != entry {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}
}

func TestSchedulerStopStopsTickerBeforeWaitingForInvocation(t *testing.T) {
	tickers := stubTickerFactory(t)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-release
		return nil
	}}

	s := newScheduler([]Job{job})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ft := <-tickers
	ft.Tick() // start the Invocation
	mustReceive(t, entered)

	stopErrCh := make(chan error, 1)
	go func() { stopErrCh <- s.Stop(context.Background()) }()

	select {
	case <-ft.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop must stop the ticker promptly, before the active Invocation finishes")
	}

	close(release)
	select {
	case err := <-stopErrCh:
		if err != nil {
			t.Fatalf("Stop error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the active Invocation finished")
	}
}

func TestSchedulerStopWaitsForActiveInvocationThenReturnsNil(t *testing.T) {
	tickers := stubTickerFactory(t)

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-release
		return nil
	}}

	s := newScheduler([]Job{job})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ft := <-tickers
	ft.Tick()
	mustReceive(t, entered)

	stopErrCh := make(chan error, 1)
	go func() { stopErrCh <- s.Stop(context.Background()) }()

	select {
	case err := <-stopErrCh:
		t.Fatalf("Stop returned early (err=%v) while an Invocation was still active", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case err := <-stopErrCh:
		if err != nil {
			t.Fatalf("Stop error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return after the active Invocation finished")
	}
}

func TestSchedulerStopReturnsStopCtxErrForUncooperativeInvocation(t *testing.T) {
	tickers := stubTickerFactory(t)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	entered := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-release // ignores ctx cancellation on purpose
		return nil
	}}

	s := newScheduler([]Job{job})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ft := <-tickers
	ft.Tick()
	mustReceive(t, entered) // the Invocation must have genuinely started before Stop begins, or Stop racing the still-in-flight Tick could legitimately discard it (see handleTick) and return nil instead of timing out

	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := s.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want errors.Is match against context.DeadlineExceeded", err)
	}
}

func TestSchedulerStopAwaitsMultipleJobsUnderOneSharedBudget(t *testing.T) {
	tickers := stubTickerFactory(t)

	fastRelease := make(chan struct{})
	slowRelease := make(chan struct{})
	t.Cleanup(func() { close(slowRelease) })
	fastEntered := make(chan struct{}, 1)
	slowEntered := make(chan struct{}, 1)

	fastJob := Job{Name: "fast", Interval: time.Minute, Run: func(context.Context) error {
		fastEntered <- struct{}{}
		<-fastRelease
		return nil
	}}
	slowJob := Job{Name: "slow", Interval: time.Minute, Run: func(context.Context) error {
		slowEntered <- struct{}{}
		<-slowRelease // outlives the shared stop-phase budget
		return nil
	}}

	s := newScheduler([]Job{fastJob, slowJob})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ftFast := <-tickers
	ftSlow := <-tickers
	ftFast.Tick()
	mustReceive(t, fastEntered)
	ftSlow.Tick()
	mustReceive(t, slowEntered) // both Invocations must have genuinely started before Stop begins — see the note in TestSchedulerStopReturnsStopCtxErrForUncooperativeInvocation

	close(fastRelease) // the fast Job finishes well within the budget

	stopCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Stop(stopCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want errors.Is match against context.DeadlineExceeded (the slow Job must not get its own separate budget)", err)
	}
}

func TestSchedulerRunsDifferentJobsIndependentlyWithoutInterference(t *testing.T) {
	tickers := stubTickerFactory(t)

	var mu sync.Mutex
	var log []string
	record := func(name string) { mu.Lock(); log = append(log, name); mu.Unlock() }

	aEntered := make(chan struct{}, 1)
	aRelease := make(chan struct{})
	jobA := Job{Name: "a", Interval: time.Minute, Run: func(context.Context) error {
		record("a")
		aEntered <- struct{}{}
		<-aRelease
		return nil
	}}

	bDone := make(chan struct{}, 1)
	jobB := Job{Name: "b", Interval: time.Minute, Run: func(context.Context) error {
		record("b")
		bDone <- struct{}{}
		return nil
	}}

	s := newScheduler([]Job{jobA, jobB})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ftA := <-tickers
	ftB := <-tickers

	ftA.Tick()
	mustReceive(t, aEntered) // job A is now blocked mid-Invocation

	// job B must still be free to run its own Invocation independently,
	// even while job A's Invocation is still active.
	ftB.Tick()
	mustReceive(t, bDone)

	close(aRelease)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(log) != 2 {
		t.Fatalf("log = %v, want both jobs to have run exactly once", log)
	}
}
