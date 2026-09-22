package tango

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/db"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

func TestWithShutdownTimeoutRejectsNonPositive(t *testing.T) {
	sqlDB := openTestDB(t)
	tests := []time.Duration{0, -time.Second}
	for _, d := range tests {
		err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0"}, sqlDB, db.SQLite, WithShutdownTimeout(d))
		if err == nil {
			t.Fatalf("WithShutdownTimeout(%s): expected an error, got nil", d)
		}
	}
}

// orderedComponent is a Lifecycle backed by a shared, mutex-protected log,
// used to assert Start/Stop ordering across multiple components.
func orderedComponent(name string, log *[]string, mu *sync.Mutex, startErr, stopErr error) Lifecycle {
	return Lifecycle{
		Name: name,
		Start: func(context.Context) error {
			mu.Lock()
			*log = append(*log, "start:"+name)
			mu.Unlock()
			return startErr
		},
		Stop: func(context.Context) error {
			mu.Lock()
			*log = append(*log, "stop:"+name)
			mu.Unlock()
			return stopErr
		},
	}
}

func TestServeContextStartOrderIsForwardStopOrderIsReverse(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	registerAll := tangoAppRegisteringLifecycles(
		orderedComponent("a", &log, &mu, nil, nil),
		orderedComponent("b", &log, &mu, nil, nil),
		orderedComponent("c", &log, &mu, nil, nil),
	)

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
	want := []string{"start:a", "start:b", "start:c", "stop:c", "stop:b", "stop:a"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

// tangoAppRegisteringLifecycles returns an App whose Register callback
// registers each given Lifecycle, in order.
func tangoAppRegisteringLifecycles(lifecycles ...Lifecycle) App {
	return NewApp("lifecycle-owner", func(r *Registry) error {
		for _, lc := range lifecycles {
			if err := r.RegisterLifecycle(lc); err != nil {
				return err
			}
		}
		return nil
	})
}

func waitDone(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("ServeContext did not return in time")
		return nil
	}
}

func TestServeContextStartFailureRollsBackAlreadyStartedComponents(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex
	startErr := errors.New("boom")

	failing := orderedComponent("b", &log, &mu, startErr, nil)
	registerAll := tangoAppRegisteringLifecycles(
		orderedComponent("a", &log, &mu, nil, nil),
		failing,
		orderedComponent("c", &log, &mu, nil, nil),
	)

	err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	if !errors.Is(err, startErr) {
		t.Fatalf("errors.Is(err, startErr) = false; err = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:a", "start:b", "stop:a"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v (component c must never start, b never stops since its own Start failed)", log, want)
	}
}

func TestServeContextCallerCancellationDuringStartupRollsBack(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())

	blockThenCancel := Lifecycle{
		Name: "blocker",
		Start: func(context.Context) error {
			mu.Lock()
			log = append(log, "start:blocker")
			mu.Unlock()
			cancel()
			return nil
		},
		Stop: func(context.Context) error {
			mu.Lock()
			log = append(log, "stop:blocker")
			mu.Unlock()
			return nil
		},
	}
	unreached := orderedComponent("unreached", &log, &mu, nil, nil)

	registerAll := tangoAppRegisteringLifecycles(blockThenCancel, unreached)

	err := ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	if err == nil {
		t.Fatal("expected an error for caller cancellation during startup, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:blocker", "stop:blocker"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v (unreached must never start)", log, want)
	}
}

func TestServeContextListenFailureTriggersRollback(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	// Occupy a port first so ServeContext's own net.Listen fails.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer occupied.Close()

	registerAll := tangoAppRegisteringLifecycles(
		orderedComponent("a", &log, &mu, nil, nil),
		orderedComponent("b", &log, &mu, nil, nil),
	)

	err = ServeContext(context.Background(), Config{Addr: occupied.Addr().String(), InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	if err == nil {
		t.Fatal("expected a listen error, got nil")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:a", "start:b", "stop:b", "stop:a"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v (every started component rolled back, reverse order)", log, want)
	}
}

func TestServeContextApplicationContextCanceledOnlyAfterDrainBeforeStop(t *testing.T) {
	sqlDB := openTestDB(t)

	var appCtx context.Context
	var canceledBeforeStop bool
	var mu sync.Mutex

	probe := Lifecycle{
		Name: "probe",
		Start: func(ctx context.Context) error {
			mu.Lock()
			appCtx = ctx
			mu.Unlock()
			return nil
		},
		Stop: func(context.Context) error {
			mu.Lock()
			canceledBeforeStop = appCtx.Err() != nil
			mu.Unlock()
			return nil
		},
	}
	registerAll := tangoAppRegisteringLifecycles(probe)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	}()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	stillLive := appCtx != nil && appCtx.Err() == nil
	mu.Unlock()
	if !stillLive {
		t.Fatal("Application context must still be live (Err() == nil) while the server is running, before shutdown begins")
	}

	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext error = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !canceledBeforeStop {
		t.Fatal("Application context must be canceled before Stop is called")
	}
}

func TestServeContextDrainAndStopPhasesAreIndependentBudgets(t *testing.T) {
	sqlDB := openTestDB(t)

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	requestReceived := make(chan struct{})

	slowRouteApp := NewApp("slow-route-app", func(r *Registry) error {
		return r.Routes().Include("/", URLs{
			Path(http.MethodGet, "/", func(c *Context) error {
				close(requestReceived)
				<-release
				return c.JSON(http.StatusOK, map[string]bool{"ok": true})
			}, Name("slow")),
		})
	})

	var stopCtxRemaining time.Duration
	var mu sync.Mutex
	probe := Lifecycle{
		Name: "probe",
		Stop: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			mu.Lock()
			if ok {
				stopCtxRemaining = time.Until(deadline)
			}
			mu.Unlock()
			return nil
		},
	}
	registerAll := tangoAppRegisteringLifecycles(probe)

	shutdownTimeout := 150 * time.Millisecond

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: addr, InstalledApps: []App{slowRouteApp, registerAll}}, sqlDB, db.SQLite, WithShutdownTimeout(shutdownTimeout))
	}()

	time.Sleep(50 * time.Millisecond)

	// Fire an in-flight request that will still be executing when we
	// cancel, forcing Shutdown to run out its full drain timeout.
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-requestReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("slow route was never hit")
	}

	start := time.Now()
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext error = %v, want nil (drain timeout forcing Close is expected, not reported as a failure)", err)
	}
	elapsed := time.Since(start)

	if elapsed < shutdownTimeout {
		t.Fatalf("shutdown returned after %s, want at least the drain timeout %s to have elapsed", elapsed, shutdownTimeout)
	}

	mu.Lock()
	defer mu.Unlock()
	if stopCtxRemaining <= 0 {
		t.Fatal("Stop's context had no remaining deadline — the stop phase must get its own fresh budget, not a shared remainder from the drain phase")
	}
	// The Stop context must have gotten close to the full configured
	// timeout, not some tiny sliver left over from draining.
	if stopCtxRemaining < shutdownTimeout/2 {
		t.Fatalf("Stop's remaining deadline = %s, want close to the full %s budget", stopCtxRemaining, shutdownTimeout)
	}
}

func TestServeContextErrorsIsFindsEachStageOfError(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex
	startErr := errors.New("start failed")
	stopErr := errors.New("stop failed")

	failing := orderedComponent("b", &log, &mu, startErr, nil)
	alsoFailsOnRollback := orderedComponent("a", &log, &mu, nil, stopErr)

	registerAll := tangoAppRegisteringLifecycles(alsoFailsOnRollback, failing)

	err := ServeContext(context.Background(), Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	if !errors.Is(err, startErr) {
		t.Fatalf("errors.Is(err, startErr) = false; err = %v", err)
	}
	if !errors.Is(err, stopErr) {
		t.Fatalf("errors.Is(err, stopErr) = false; err = %v", err)
	}
}

// settledGoroutineCount lets the runtime finish tearing down anything
// already scheduled to exit (closed listener connections, etc.) before
// sampling, so the count reflects steady state rather than a race with
// in-flight cleanup.
func settledGoroutineCount() int {
	var n int
	for i := 0; i < 50; i++ {
		runtime.Gosched()
		n = runtime.NumGoroutine()
	}
	return n
}

func TestServeContextFullCycleLeavesNoGoroutinesBehind(t *testing.T) {
	sqlDB := openTestDB(t)

	before := settledGoroutineCount()

	var log []string
	var mu sync.Mutex
	registerAll := tangoAppRegisteringLifecycles(
		orderedComponent("a", &log, &mu, nil, nil),
		orderedComponent("b", &log, &mu, nil, nil),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite, WithShutdownTimeout(200*time.Millisecond))
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext error = %v, want nil", err)
	}

	time.Sleep(50 * time.Millisecond)
	after := settledGoroutineCount()

	if after > before {
		t.Fatalf("goroutine count after a full start->serve->shutdown cycle = %d, want <= %d (before)", after, before)
	}
}
