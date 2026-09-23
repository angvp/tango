package tango

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/angvp/tango/internal/observabilitysafe"
	"github.com/angvp/tango/observability"
)

type jobObserver struct {
	logger   *slog.Logger
	recorder observability.Recorder
}

func newJobObserver(logger *slog.Logger, recorder observability.Recorder) jobObserver {
	if logger == nil {
		logger = slog.Default()
	}
	if recorder == nil {
		recorder = observability.NopRecorder{}
	}
	return jobObserver{logger: logger, recorder: recorder}
}

// runJobLoop builds job's ticker, owns stopping it on exit, and runs its
// loop. Used where ticker creation order doesn't matter (a single Job loop
// in isolation); scheduler Start instead creates each Job's ticker
// synchronously, in registration order, via newTicker directly, and stops
// every ticker itself from Scheduler.Stop (see scheduler.go) rather than
// leaving that to the Job loop — so which Job's ticker is created first is
// never left to goroutine scheduling, and every ticker is stopped before
// shutdown waiting begins, not merely eventually as each Job loop notices
// stopCh.
func runJobLoop(ctx context.Context, job Job, stopCh <-chan struct{}, wg *sync.WaitGroup) {
	runJobLoopObserved(ctx, job, stopCh, wg, newJobObserver(slog.Default(), observability.NopRecorder{}))
}

func runJobLoopObserved(ctx context.Context, job Job, stopCh <-chan struct{}, wg *sync.WaitGroup, observer jobObserver) {
	defer wg.Done()
	ticker := newTicker(job.Interval)
	defer ticker.Stop()
	runJobLoopWithTickerObserved(ctx, job, ticker, stopCh, observer)
}

// jobLoopReady is called once, synchronously, immediately before a Job
// loop's first select iteration — before it has looked at ticker.C() or
// stopCh at all. Package-private; tests override it to gate a Job loop
// goroutine deterministically (e.g. to arrange a pending Tick and an
// already-closed stopCh before letting it run at all), without depending
// on goroutine-scheduling timing to land a race window.
var jobLoopReady = func() {}

// runJobLoopWithTicker is the sole owner of one Job's active/idle state. It
// runs until stopCh is closed; stopping ticker itself is the caller's
// responsibility (see runJobLoop and scheduler.Stop), not this loop's.
//
// On a Tick while idle, it starts exactly one Invocation (a single call to
// job.Run, against ctx) — see handleTick. On a Tick while an Invocation is
// active, the Tick is discarded permanently — not queued, not replayed, no
// signal of any kind. An Invocation completing does not itself consume a
// buffered Tick; only a genuinely new Tick, occurring after completion,
// starts the next Invocation.
func runJobLoopWithTicker(ctx context.Context, job Job, ticker tickerSource, stopCh <-chan struct{}) {
	runJobLoopWithTickerObserved(ctx, job, ticker, stopCh, newJobObserver(slog.Default(), observability.NopRecorder{}))
}

func runJobLoopWithTickerObserved(ctx context.Context, job Job, ticker tickerSource, stopCh <-chan struct{}, observer jobObserver) {
	jobLoopReady()

	// Non-nil exactly while an Invocation is active; nil disables the
	// corresponding select case, so a completed Invocation is never
	// re-observed and a Tick can never be mistaken for one.
	var invocationDone chan struct{}

	for {
		// Checked non-blockingly before the main select so that, once
		// shutdown begins, this Job loop deterministically stops picking
		// up any further Tick — including one already ready — rather than
		// leaving that choice to Go's random tie-breaking between two
		// simultaneously ready select cases.
		select {
		case <-stopCh:
			if invocationDone != nil {
				<-invocationDone
			}
			return
		default:
		}

		select {
		case <-stopCh:
			if invocationDone != nil {
				<-invocationDone
			}
			return

		case <-ticker.C():
			if invocationDone != nil {
				continue
			}
			invocationDone = handleTickObserved(ctx, job, stopCh, observer)

		case <-invocationDone:
			invocationDone = nil
		}
	}
}

// handleTick processes a Tick the main select in runJobLoopWithTicker just
// received — except that select can itself race shutdown: both its
// ticker.C() and stopCh cases can become ready at once (a Tick may already
// be buffered in the ticker's channel even after Stop, since stopping a
// ticker does not drain it), and Go's select makes no promise about which
// one it picks. Re-checking stopCh here, after the Tick was received but
// before starting an Invocation, closes that window: if stopCh is already
// closed, the Tick is discarded instead of starting a new Invocation. It
// returns the Invocation's completion channel, or nil if none was started.
func handleTick(ctx context.Context, job Job, stopCh <-chan struct{}) chan struct{} {
	return handleTickObserved(ctx, job, stopCh, newJobObserver(slog.Default(), observability.NopRecorder{}))
}

func handleTickObserved(ctx context.Context, job Job, stopCh <-chan struct{}, observer jobObserver) chan struct{} {
	select {
	case <-stopCh:
		return nil
	default:
	}

	done := make(chan struct{})
	go runInvocation(ctx, job, done, observer)
	return done
}

// runInvocation makes one call to job.Run, recovering any panic and
// reporting a returned error or recovered panic through observer, then
// closes done. ctx is the Application context; a returned error matching
// its own cancellation is logged as no failure and recorded as canceled.
type jobPanicError struct {
	value any
	stack []byte
}

func (e *jobPanicError) Error() string {
	return fmt.Sprintf("tango: job panicked: %v\n%s", e.value, e.stack)
}

func runInvocation(ctx context.Context, job Job, done chan struct{}, observer jobObserver) {
	defer close(done)
	defer func() {
		if r := recover(); r != nil {
			err := &jobPanicError{value: r, stack: debug.Stack()}
			reportJobError(ctx, job.Name, err, true, observer)
			recordJobOutcome(job.Name, "panic", observer)
		}
	}()

	if err := job.Run(ctx); err != nil {
		if appErr := ctx.Err(); appErr != nil && errors.Is(err, appErr) {
			recordJobOutcome(job.Name, "canceled", observer)
			return
		}
		reportJobError(ctx, job.Name, err, false, observer)
		recordJobOutcome(job.Name, "error", observer)
		return
	}
	recordJobOutcome(job.Name, "success", observer)
}

func reportJobError(ctx context.Context, name string, err error, panicked bool, observer jobObserver) {
	observabilitysafe.Call(func() {
		observer.logger.LogAttrs(ctx, slog.LevelError, EventJobFailed,
			slog.String("job", name), slog.Any("error", err), slog.Bool("panicked", panicked))
	})
}

func recordJobOutcome(name, outcome string, observer jobObserver) {
	observabilitysafe.Call(func() {
		observer.recorder.AddCounter(observability.MetricSchedulerJobInvocations, 1,
			slog.String("job", name), slog.String("outcome", outcome))
	})
}
