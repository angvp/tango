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

// captureListenerAddr overrides the package-private newListener seam for
// the duration of the test, sending the real bound address to the returned
// channel the moment binding succeeds — lets a test dial a ServeContext
// started on "127.0.0.1:0" without a fixed sleep or a separate racy
// reserve-then-reuse-the-port dance.
func captureListenerAddr(t *testing.T) <-chan string {
	t.Helper()
	addrCh := make(chan string, 1)
	orig := newListener
	newListener = func(network, address string) (net.Listener, error) {
		ln, err := orig(network, address)
		if err == nil {
			addrCh <- ln.Addr().String()
		}
		return ln, err
	}
	t.Cleanup(func() { newListener = orig })
	return addrCh
}

// triggerFailListener wraps a real net.Listener so a test can force the
// next Accept call to fail with a controlled sentinel error on demand
// (closing trigger), even if a real Accept is already blocked waiting for
// a connection — used to exercise Server.Serve returning a genuine,
// non-http.ErrServerClosed error after startup has already succeeded.
type triggerFailListener struct {
	net.Listener
	trigger chan struct{}
	failErr error
}

func (l *triggerFailListener) Accept() (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := l.Listener.Accept()
		ch <- result{conn, err}
	}()
	select {
	case <-l.trigger:
		return nil, l.failErr
	case r := <-ch:
		return r.conn, r.err
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

// TestServeContextCallerCancellationWhileStartIsBlockedRollsBackWithoutLeakingGoroutine
// covers the case the above test does not: a Start that is actually
// *blocked*, cooperatively waiting on the context it was given (the
// Application context), rather than one that triggers cancellation itself
// and returns immediately. Before this fix, ServeContext only checked for
// caller cancellation *before* each Start and then called Start
// synchronously — a Start blocked on <-ctx.Done() never observed
// cancellation (the Application context is deliberately detached from the
// caller's), so ServeContext could hang forever.
func TestServeContextCallerCancellationWhileStartIsBlockedRollsBackWithoutLeakingGoroutine(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	ctx, cancel := context.WithCancel(context.Background())

	startRunning := make(chan struct{})
	startReturned := make(chan struct{})
	blocker := Lifecycle{
		Name: "blocker",
		Start: func(startCtx context.Context) error {
			mu.Lock()
			log = append(log, "start:blocker")
			mu.Unlock()
			close(startRunning)
			<-startCtx.Done() // cooperative: waits on the Application context
			close(startReturned)
			return startCtx.Err()
		},
		Stop: func(context.Context) error {
			mu.Lock()
			log = append(log, "stop:blocker")
			mu.Unlock()
			return nil
		},
	}
	unreached := orderedComponent("unreached", &log, &mu, nil, nil)
	registerAll := tangoAppRegisteringLifecycles(blocker, unreached)

	before := settledGoroutineCount()

	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	}()

	<-startRunning
	cancel()

	select {
	case <-startReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Start never observed the Application context's cancellation — ServeContext must cancel it promptly while Start is running")
	}

	err := waitDone(t, done)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}

	mu.Lock()
	gotLog := append([]string(nil), log...)
	mu.Unlock()
	// blocker's own Start was the one interrupted — it must not be treated
	// as successfully started, so its Stop must never run, and unreached
	// must never start.
	want := []string{"start:blocker"}
	if fmt.Sprint(gotLog) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v", gotLog, want)
	}

	// Give any would-be leaked goroutine a moment to show up before
	// counting — ServeContext must already have waited for the blocked
	// Start to actually return before returning itself, so there should be
	// nothing left to settle.
	time.Sleep(20 * time.Millisecond)
	after := settledGoroutineCount()
	if after > before {
		t.Fatalf("goroutine count after cancellation while Start was blocked = %d, want <= %d (before) — the startup goroutine running Start must not leak", after, before)
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

// TestServeContextDrainTimeoutForcesCloseAndReturnsWrappedDeadlineExceeded
// covers the drain-timeout contract: hitting the drain deadline forces a
// Server.Close, but the timeout itself is still reported (wrapped, so
// errors.Is(err, context.DeadlineExceeded) still finds it) rather than
// hidden just because the forced close succeeded — shutdown was not fully
// graceful, in-flight requests were aborted. It also proves the
// Application context stays live for the whole drain window (not just
// before cancellation), is canceled only afterward, and that the stop
// phase still gets its own independent budget.
func TestServeContextDrainTimeoutForcesCloseAndReturnsWrappedDeadlineExceeded(t *testing.T) {
	sqlDB := openTestDB(t)

	release := make(chan struct{})
	defer close(release)
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

	var appCtx context.Context
	var stopCtxRemaining time.Duration
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
		Stop: func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			mu.Lock()
			if ok {
				stopCtxRemaining = time.Until(deadline)
			}
			canceledBeforeStop = appCtx.Err() != nil
			mu.Unlock()
			return nil
		},
	}
	registerAll := tangoAppRegisteringLifecycles(probe)

	shutdownTimeout := 150 * time.Millisecond

	addrCh := captureListenerAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{slowRouteApp, registerAll}}, sqlDB, db.SQLite, WithShutdownTimeout(shutdownTimeout))
	}()
	addr := <-addrCh

	requestErrCh := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
		requestErrCh <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("slow route was never hit")
	}

	mu.Lock()
	liveBeforeCancel := appCtx != nil && appCtx.Err() == nil
	mu.Unlock()
	if !liveBeforeCancel {
		t.Fatal("Application context must be live before shutdown begins")
	}

	start := time.Now()
	cancel()

	// The request is still blocked on <-release, so draining is still in
	// progress at this point (well before the drain deadline) — the
	// Application context must still be live *during* draining, not just
	// observed live once before cancellation.
	time.Sleep(shutdownTimeout / 3)
	mu.Lock()
	liveDuringDrain := appCtx.Err() == nil
	mu.Unlock()
	if !liveDuringDrain {
		t.Fatal("Application context must still be live (Err() == nil) while a request is actively draining")
	}

	err := waitDone(t, done)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false; err = %v, want the drain-timeout error reported even though the forced close itself succeeded", err)
	}
	if elapsed < shutdownTimeout {
		t.Fatalf("shutdown returned after %s, want at least the drain timeout %s to have elapsed", elapsed, shutdownTimeout)
	}

	if requestErr := <-requestErrCh; requestErr == nil {
		t.Fatal("the still-blocked request should have been aborted by the forced Server.Close, not completed successfully — proves the forced close actually happened")
	}

	mu.Lock()
	defer mu.Unlock()
	if !canceledBeforeStop {
		t.Fatal("Application context must be canceled before Stop runs")
	}
	if stopCtxRemaining <= 0 {
		t.Fatal("Stop's context had no remaining deadline — the stop phase must get its own fresh budget, independent from the drain phase")
	}
	if stopCtxRemaining < shutdownTimeout/2 {
		t.Fatalf("Stop's remaining deadline = %s, want close to the full %s budget (independent from the drain phase)", stopCtxRemaining, shutdownTimeout)
	}
}

// TestServeContextStopPhaseSharesOneTimeoutBudgetAcrossComponents proves
// the whole reverse-order Stop pass shares a single timeout-bounded
// context — not one fresh full budget handed to every individual
// component, which would let total shutdown time grow without bound as
// more Lifecycles are registered.
func TestServeContextStopPhaseSharesOneTimeoutBudgetAcrossComponents(t *testing.T) {
	sqlDB := openTestDB(t)
	shutdownTimeout := 300 * time.Millisecond
	perStopSleep := 60 * time.Millisecond

	var remaining []time.Duration
	var mu sync.Mutex
	makeStop := func(name string) Lifecycle {
		return Lifecycle{
			Name: name,
			Stop: func(ctx context.Context) error {
				time.Sleep(perStopSleep)
				deadline, ok := ctx.Deadline()
				mu.Lock()
				if ok {
					remaining = append(remaining, time.Until(deadline))
				}
				mu.Unlock()
				return nil
			},
		}
	}

	ready := make(chan struct{})
	readySignal := Lifecycle{Name: "ready", Start: func(context.Context) error { close(ready); return nil }}

	registerAll := tangoAppRegisteringLifecycles(makeStop("a"), makeStop("b"), makeStop("c"), readySignal)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ServeContext(ctx, Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite, WithShutdownTimeout(shutdownTimeout))
	}()

	<-ready
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Fatalf("ServeContext error = %v, want nil", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Reverse registration order: readySignal (no Stop, skipped), then c,
	// b, a — remaining[0] is c's sample, remaining[2] is a's.
	if len(remaining) != 3 {
		t.Fatalf("got %d Stop deadline samples, want 3", len(remaining))
	}
	first, last := remaining[0], remaining[2]
	if first <= last {
		t.Fatalf("remaining deadlines = %v, want strictly decreasing across calls (proves one shared, draining budget) not a per-call reset", remaining)
	}
	// If each Stop got its own fresh full budget, later calls would still
	// see close to the full shutdownTimeout regardless of how long earlier
	// calls took. Sharing one budget means each subsequent call sees
	// roughly perStopSleep less than the one before it.
	drop := first - last
	wantMinDrop := perStopSleep // generous slack below the ideal 2*perStopSleep
	if drop < wantMinDrop {
		t.Fatalf("remaining deadline dropped by only %s across 3 calls (%v), want at least %s if the budget is truly shared", drop, remaining, wantMinDrop)
	}
}

// TestServeContextServeErrorAfterStartupStopsComponentsAndPreservesError
// covers a genuine (non-http.ErrServerClosed) error returned by
// Server.Serve after startup already succeeded — e.g. an Accept failure
// unrelated to a caller-triggered shutdown. It must still be treated as a
// real failure: HTTP finalization runs, the Application context is
// canceled before lifecycle stopping, every started component is stopped
// in reverse order, and the original serving error stays discoverable via
// errors.Is even alongside any Stop errors.
func TestServeContextServeErrorAfterStartupStopsComponentsAndPreservesError(t *testing.T) {
	sqlDB := openTestDB(t)
	var log []string
	var mu sync.Mutex

	var appCtx context.Context
	var canceledBeforeStop bool
	probe := Lifecycle{
		Name: "probe",
		Start: func(ctx context.Context) error {
			mu.Lock()
			appCtx = ctx
			log = append(log, "start:probe")
			mu.Unlock()
			return nil
		},
		Stop: func(context.Context) error {
			mu.Lock()
			canceledBeforeStop = appCtx.Err() != nil
			log = append(log, "stop:probe")
			mu.Unlock()
			return nil
		},
	}
	registerAll := tangoAppRegisteringLifecycles(
		orderedComponent("a", &log, &mu, nil, nil),
		probe,
	)

	injectedErr := errors.New("injected serve failure")
	trigger := make(chan struct{})
	addrCh := make(chan string, 1)

	origNewListener := newListener
	newListener = func(network, address string) (net.Listener, error) {
		ln, err := origNewListener(network, address)
		if err != nil {
			return nil, err
		}
		addrCh <- ln.Addr().String()
		return &triggerFailListener{Listener: ln, trigger: trigger, failErr: injectedErr}, nil
	}
	t.Cleanup(func() { newListener = origNewListener })

	done := make(chan error, 1)
	go func() {
		done <- ServeContext(context.Background(), Config{Addr: "127.0.0.1:0", InstalledApps: []App{registerAll}}, sqlDB, db.SQLite)
	}()

	addr := <-addrCh

	// Confirm the server is genuinely accepting connections through the
	// real listener before injecting the failure.
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("initial request before injected failure: %v", err)
	}
	resp.Body.Close()

	close(trigger)

	err = waitDone(t, done)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("errors.Is(err, injectedErr) = false; err = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start:a", "start:probe", "stop:probe", "stop:a"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	if !canceledBeforeStop {
		t.Fatal("Application context must be canceled before Stop runs, even on the serve-error path")
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
