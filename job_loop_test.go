package tango

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angvp/tango/observability"
)

// fakeTicker is a fully test-controlled tickerSource: Tick() delivers
// exactly one tick, blocking until the Job loop's select consumes it (so
// tests never need a real sleep or a short-duration real ticker to prove
// scheduling semantics), and Stop() is observable via the stopped channel.
type fakeTicker struct {
	tick    chan time.Time
	stopped chan struct{}
	once    sync.Once
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{tick: make(chan time.Time), stopped: make(chan struct{})}
}

func (f *fakeTicker) C() <-chan time.Time { return f.tick }

func (f *fakeTicker) Stop() {
	f.once.Do(func() { close(f.stopped) })
}

// Tick delivers one tick, blocking until it's consumed.
func (f *fakeTicker) Tick() {
	f.tick <- time.Now()
}

// stubSingleTicker overrides the package-private newTicker seam so every
// call returns ft — for tests driving exactly one Job loop.
func stubSingleTicker(t *testing.T, ft *fakeTicker) {
	t.Helper()
	orig := newTicker
	newTicker = func(time.Duration) tickerSource { return ft }
	t.Cleanup(func() { newTicker = orig })
}

// stubTickerFactory overrides newTicker so each call returns a fresh
// fakeTicker, delivered over the returned channel in call order — for
// tests driving multiple Job loops' tickers independently.
func stubTickerFactory(t *testing.T) <-chan *fakeTicker {
	t.Helper()
	ch := make(chan *fakeTicker, 16)
	orig := newTicker
	newTicker = func(time.Duration) tickerSource {
		ft := newFakeTicker()
		ch <- ft
		return ft
	}
	t.Cleanup(func() { newTicker = orig })
	return ch
}

// reportedFailure records one EventJobFailed log event.
type reportedFailure struct {
	name string
	err  error
}

type jobCaptureHandler struct{ reports chan reportedFailure }

func (h *jobCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *jobCaptureHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *jobCaptureHandler) WithGroup(string) slog.Handler            { return h }
func (h *jobCaptureHandler) Handle(_ context.Context, record slog.Record) error {
	report := reportedFailure{}
	record.Attrs(func(attr slog.Attr) bool {
		switch attr.Key {
		case "job":
			report.name = attr.Value.String()
		case "error":
			if value, ok := attr.Value.Any().(error); ok {
				report.err = value
			}
		}
		return true
	})
	h.reports <- report
	return nil
}

func captureJobFailures(t *testing.T) (jobObserver, chan reportedFailure) {
	t.Helper()
	ch := make(chan reportedFailure, 16)
	logger := slog.New(&jobCaptureHandler{reports: ch})
	return newJobObserver(logger, observability.NopRecorder{}), ch
}

func mustReceive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("expected signal was not received in time")
	}
}

func mustReceiveReport(t *testing.T, ch <-chan reportedFailure) reportedFailure {
	t.Helper()
	select {
	case rep := <-ch:
		return rep
	case <-time.After(2 * time.Second):
		t.Fatal("expected EventJobFailed log was not received in time")
		return reportedFailure{}
	}
}

// retryTickUntilStarted offers Ticks to ft, retrying briefly, until one
// lands after the Job loop has fully processed a prior Invocation's
// completion and gone idle again. A Tick offered in the brief in-between
// window is legitimately discarded (proven independently by
// TestJobLoopDiscardsTicksWhileInvocationIsActive) — this only resolves the
// test's own synchronization gap between releasing an Invocation and
// observing the Job loop consume its completion, and asserts nothing about
// real elapsed time or interval semantics.
func retryTickUntilStarted(t *testing.T, ft *fakeTicker, entered <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ft.Tick()
		select {
		case <-entered:
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Fatal("a new Invocation never started from a Tick following the prior one's completion")
}

func TestJobLoopWaitsForTickBeforeFirstInvocation(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)

	runCalled := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		runCalled <- struct{}{}
		return nil
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoop(context.Background(), job, stopCh, &wg)

	select {
	case <-runCalled:
		t.Fatal("Run must not be called before the first Tick")
	default:
	}

	close(stopCh)
	wg.Wait()

	select {
	case <-runCalled:
		t.Fatal("Run must not be called before the first Tick")
	default:
	}
}

func TestJobLoopUsesJobIntervalForTicker(t *testing.T) {
	var gotInterval time.Duration
	ft := newFakeTicker()
	orig := newTicker
	newTicker = func(d time.Duration) tickerSource {
		gotInterval = d
		return ft
	}
	t.Cleanup(func() { newTicker = orig })

	job := Job{Name: "j", Interval: 37 * time.Second, Run: func(context.Context) error { return nil }}
	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoop(context.Background(), job, stopCh, &wg)
	close(stopCh)
	wg.Wait()

	if gotInterval != job.Interval {
		t.Fatalf("newTicker interval = %s, want %s", gotInterval, job.Interval)
	}
}

func TestJobLoopStartsExactlyOneInvocationPerIdleTick(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)

	calls := make(chan struct{}, 4)
	released := make(chan struct{})
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		calls <- struct{}{}
		<-released
		return nil
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoop(context.Background(), job, stopCh, &wg)

	ft.Tick()
	mustReceive(t, calls)

	close(released)
	close(stopCh)
	wg.Wait()
}

func TestJobLoopDiscardsTicksWhileInvocationIsActive(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)

	entered := make(chan struct{}, 4)
	released := make(chan struct{})
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-released
		return nil
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoop(context.Background(), job, stopCh, &wg)

	ft.Tick()
	mustReceive(t, entered) // Invocation 1 started

	ft.Tick() // discarded: Invocation 1 still active
	ft.Tick() // discarded: Invocation 1 still active

	select {
	case <-entered:
		t.Fatal("a Tick received while an Invocation is active must not start another Invocation")
	default:
	}

	close(released)
	close(stopCh)
	wg.Wait()
}

func TestJobLoopCompletionDoesNotAutoStartWithoutAGenuinelyNewTick(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)

	entered := make(chan struct{}, 4)
	released := make(chan struct{})
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-released
		return nil
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoop(context.Background(), job, stopCh, &wg)

	ft.Tick()
	mustReceive(t, entered) // Invocation 1 started

	ft.Tick() // discarded while Invocation 1 is active
	released <- struct{}{}

	// A genuinely new Tick after completion must still start a second
	// Invocation — proving the discarded Tick above was not silently
	// "saved" and replayed on completion.
	retryTickUntilStarted(t, ft, entered)

	select {
	case <-entered:
		t.Fatal("more Invocations started than Ticks genuinely delivered after completion")
	default:
	}

	released <- struct{}{}
	close(stopCh)
	wg.Wait()
}

func TestJobLoopReportsReturnedErrorWithoutStoppingTheLoop(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	wantErr := errors.New("boom")
	calls := make(chan struct{}, 4)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		calls <- struct{}{}
		return wantErr
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(context.Background(), job, stopCh, &wg, observer)

	ft.Tick()
	mustReceive(t, calls)

	rep := mustReceiveReport(t, reports)
	if rep.name != "j" || !errors.Is(rep.err, wantErr) {
		t.Fatalf("reported = %+v, want name=%q err wrapping %v", rep, "j", wantErr)
	}

	// The loop must still be alive: a further genuinely new Tick starts
	// another Invocation.
	retryTickUntilStarted(t, ft, calls)

	close(stopCh)
	wg.Wait()
}

func TestJobLoopReportsPanicWithRecoveredValueAndStack(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		panic("boom")
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(context.Background(), job, stopCh, &wg, observer)

	ft.Tick()
	rep := mustReceiveReport(t, reports)
	if rep.name != "j" {
		t.Fatalf("reported name = %q, want %q", rep.name, "j")
	}
	if !strings.Contains(rep.err.Error(), "boom") {
		t.Fatalf("reported err = %v, want it to contain the recovered value %q", rep.err, "boom")
	}
	if !strings.Contains(rep.err.Error(), "goroutine") {
		t.Fatalf("reported err = %v, want it to contain stack trace information", rep.err)
	}

	close(stopCh)
	wg.Wait()
}

func TestJobLoopSuppressesErrorMatchingApplicationContextCancellation(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	appCtx, cancelApp := context.WithCancel(context.Background())
	calls := make(chan struct{}, 4)
	job := Job{Name: "j", Interval: time.Minute, Run: func(ctx context.Context) error {
		calls <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(appCtx, job, stopCh, &wg, observer)

	ft.Tick()
	mustReceive(t, calls)
	cancelApp()

	select {
	case rep := <-reports:
		t.Fatalf("an error matching the Application context's own cancellation must be suppressed, got: %+v", rep)
	case <-time.After(200 * time.Millisecond):
	}

	close(stopCh)
	wg.Wait()
}

func TestJobLoopStillReportsUnrelatedErrorWhenApplicationContextCanceled(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	appCtx, cancelApp := context.WithCancel(context.Background())
	cancelApp()

	wantErr := errors.New("boom")
	calls := make(chan struct{}, 4)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		calls <- struct{}{}
		return wantErr
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(appCtx, job, stopCh, &wg, observer)

	ft.Tick()
	mustReceive(t, calls)

	rep := mustReceiveReport(t, reports)
	if !errors.Is(rep.err, wantErr) {
		t.Fatalf("reported err = %v, want errors.Is match against %v (an unrelated error must still be reported)", rep.err, wantErr)
	}

	close(stopCh)
	wg.Wait()
}

func TestJobLoopStillReportsPanicWhenApplicationContextCanceled(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	appCtx, cancelApp := context.WithCancel(context.Background())
	cancelApp()

	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		panic("boom")
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(appCtx, job, stopCh, &wg, observer)

	ft.Tick()
	rep := mustReceiveReport(t, reports)
	if !strings.Contains(rep.err.Error(), "boom") {
		t.Fatalf("reported err = %v, want it to contain the recovered value %q (a panic must never be suppressed)", rep.err, "boom")
	}

	close(stopCh)
	wg.Wait()
}

func TestJobLoopNeverReportsSkippedTicks(t *testing.T) {
	ft := newFakeTicker()
	stubSingleTicker(t, ft)
	observer, reports := captureJobFailures(t)

	entered := make(chan struct{}, 4)
	released := make(chan struct{})
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		entered <- struct{}{}
		<-released
		return nil
	}}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go runJobLoopObserved(context.Background(), job, stopCh, &wg, observer)

	ft.Tick()
	mustReceive(t, entered)
	ft.Tick() // discarded
	ft.Tick() // discarded
	close(released)
	close(stopCh)
	wg.Wait()

	select {
	case rep := <-reports:
		t.Fatalf("skipped Ticks must never emit EventJobFailed, got: %+v", rep)
	default:
	}
}

// TestHandleTickDiscardsATickReceivedAfterStopChIsAlreadyClosed proves
// handleTick's defensive re-check directly and deterministically, the same
// way realtime's TestReceivedOpAfterCloseChIsRejectedWithoutRunningLogic
// proves handleReceivedOp's: rather than racing goroutines against Go's
// select and hoping to land on the ambiguous tie-break window, it puts
// stopCh into the exact state that window can produce (already closed) and
// calls the post-receive handler directly, as if a Tick had just been
// "received" from the main select at the same moment shutdown began.
func TestHandleTickDiscardsATickReceivedAfterStopChIsAlreadyClosed(t *testing.T) {
	runCalled := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		runCalled <- struct{}{}
		return nil
	}}

	stopCh := make(chan struct{})
	close(stopCh)

	done := handleTick(context.Background(), job, stopCh)
	if done != nil {
		t.Fatal("handleTick must not start an Invocation once stopCh is already closed")
	}

	select {
	case <-runCalled:
		t.Fatal("Run must never be called for a Tick caught by the shutdown race")
	default:
	}
}

// bufferedFakeTicker is a tickerSource whose channel has a one-slot buffer,
// like a real time.Ticker's. Unlike fakeTicker.Tick (which deliberately
// blocks until the Job loop consumes it, and is the synchronization
// primitive most other tests in this file rely on), a send here always
// completes immediately — used only to deterministically arrange a Tick
// that's genuinely "already pending" before shutdown begins, with nothing
// to synchronize on and no separate goroutine needed.
type bufferedFakeTicker struct {
	tick    chan time.Time
	stopped chan struct{}
}

func newBufferedFakeTicker() *bufferedFakeTicker {
	return &bufferedFakeTicker{tick: make(chan time.Time, 1), stopped: make(chan struct{})}
}

func (b *bufferedFakeTicker) C() <-chan time.Time { return b.tick }

func (b *bufferedFakeTicker) Stop() {
	select {
	case <-b.stopped:
	default:
		close(b.stopped)
	}
}

// TestSchedulerStopDiscardsATickAlreadyPendingWhenShutdownBegins exercises
// the race end to end through the real scheduler and a fake ticker. The Job
// loop goroutine is gated on jobLoopReady so it cannot look at either
// channel until released — which lets the test arrange, with certainty and
// no timing dependence, both a genuinely pending Tick and an already-closed
// stopCh before the Job loop's select ever runs at all. (An earlier version
// of this test tried to arrange that race by timing alone — planting a
// buffered Tick and calling Stop immediately after, with nothing gating the
// Job loop goroutine — and failed under repetition: on a multi-core
// machine, the already-running Job loop goroutine can consume and fully
// process a buffered Tick, on another core, before the test's own next
// statement calls Stop at all. Gating on jobLoopReady removes that
// dependency entirely.)
//
// Whichever way the Job loop's own select actually resolves the tie between
// its ticker.C() and stopCh cases once released, the outcome must be
// identical — no Invocation starts, the ticker is stopped, and Stop returns
// cleanly — which TestHandleTickDiscardsATickReceivedAfterStopChIsAlreadyClosed
// above proves is guaranteed by construction; this test is the
// integration-level confirmation, and repeating it (see validation) is only
// an additional stress check, not the primary correctness proof.
func TestSchedulerStopDiscardsATickAlreadyPendingWhenShutdownBegins(t *testing.T) {
	bft := newBufferedFakeTicker()
	origTicker := newTicker
	newTicker = func(time.Duration) tickerSource { return bft }
	t.Cleanup(func() { newTicker = origTicker })

	release := make(chan struct{})
	origReady := jobLoopReady
	jobLoopReady = func() { <-release }
	t.Cleanup(func() { jobLoopReady = origReady })

	runCalled := make(chan struct{}, 1)
	job := Job{Name: "j", Interval: time.Minute, Run: func(context.Context) error {
		runCalled <- struct{}{}
		return nil
	}}

	s := newScheduler([]Job{job})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The Job loop goroutine is blocked in jobLoopReady and has not looked
	// at ticker.C() or stopCh yet, so both of the next two steps can be
	// arranged with no race against it whatsoever.
	bft.tick <- time.Now() // plant a genuinely pending Tick — never blocks

	stopErrCh := make(chan error, 1)
	go func() { stopErrCh <- s.Stop(context.Background()) }()

	select {
	case <-s.stopCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not close stopCh in time")
	}

	// Both a pending Tick and an already-closed stopCh are now in place —
	// exactly the state the Job loop's select can race between. Only now
	// does the Job loop get to run its select at all and resolve that tie,
	// however Go's runtime picks.
	close(release)

	select {
	case err := <-stopErrCh:
		if err != nil {
			t.Fatalf("Stop error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time")
	}

	select {
	case <-bft.stopped:
	default:
		t.Fatal("Stop must stop every Job's ticker")
	}

	select {
	case <-runCalled:
		t.Fatal("a Tick already pending when shutdown begins must never start a new Invocation")
	default:
	}
}
