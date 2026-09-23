package tango

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/angvp/tango/observability"
)

// schedulerLifecycleName is the reserved Lifecycle Name ServeContext uses
// for the Scheduler lifecycle it appends itself. RegisterLifecycle rejects
// any host registration using this exact Name (ErrReservedLifecycleName),
// and it is never returned by Registry.Lifecycles().
const schedulerLifecycleName = "tango: scheduler"

// tickerSource abstracts time.Ticker so tests can inject fully controlled,
// manually-driven tick delivery — no real sleeps, no short-duration real
// tickers — without a public clock/ticker option on ServeContext.
type tickerSource interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

// newTicker is the seam every Job loop binds through. Overridden in tests.
var newTicker = func(d time.Duration) tickerSource {
	return realTicker{time.NewTicker(d)}
}

// scheduler owns one Job loop per registered Job, and the ticker each loop
// runs against. It is the Scheduler lifecycle's Start/Stop implementation.
//
// tickers is retained (not left for each Job loop to own and stop for
// itself) specifically so Stop can stop every ticker directly, itself,
// before signaling any Job loop to shut down at all: a Job loop noticing
// stopCh and stopping its own ticker only afterward would leave a window
// where a Tick already in flight races shutdown in that Job loop's select.
// Stopping every ticker first narrows, but does not by itself close, that
// window — stopping a ticker does not drain a Tick already buffered in its
// channel — so runJobLoopWithTicker additionally re-checks stopCh itself
// before starting an Invocation (see handleTick).
type scheduler struct {
	jobs     []Job
	stopCh   chan struct{}
	wg       sync.WaitGroup
	tickers  []tickerSource
	observer jobObserver
}

func newScheduler(jobs []Job, observers ...jobObserver) *scheduler {
	observer := newJobObserver(slog.Default(), observability.NopRecorder{})
	if len(observers) != 0 {
		observer = observers[0]
	}
	return &scheduler{jobs: jobs, stopCh: make(chan struct{}), observer: observer}
}

// Start spawns one Job loop goroutine per registered Job, each running
// against ctx (the long-lived Application context) for the life of the
// process. Each Job's ticker is created synchronously, in registration
// order, before its loop goroutine is spawned — so newTicker call order
// always matches registration order, never goroutine scheduling order —
// and retained in s.tickers so Stop can stop every one of them directly.
func (s *scheduler) Start(ctx context.Context) error {
	s.tickers = make([]tickerSource, len(s.jobs))
	for i, job := range s.jobs {
		ticker := newTicker(job.Interval)
		s.tickers[i] = ticker
		s.wg.Add(1)
		go func(job Job, ticker tickerSource) {
			defer s.wg.Done()
			runJobLoopWithTickerObserved(ctx, job, ticker, s.stopCh, s.observer)
		}(job, ticker)
	}
	return nil
}

// Stop stops every Job's ticker directly, itself — so no Job loop can
// observe a fresh Tick once Stop has begun — then closes stopCh so every
// Job loop begins shutting down: discarding a Tick it already has in hand
// (see handleTick) rather than starting a new Invocation from it, and
// waiting for any Invocation it already has active. It returns nil once
// every Job loop has finished, or stopCtx.Err() promptly if stopCtx expires
// first; it never forcibly kills a goroutine, so an uncooperative Run may
// remain alive in the background after Stop returns.
func (s *scheduler) Stop(stopCtx context.Context) error {
	for _, ticker := range s.tickers {
		ticker.Stop()
	}
	close(s.stopCh)

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-stopCtx.Done():
		return stopCtx.Err()
	}
}

// newSchedulerLifecycle builds the Scheduler lifecycle ServeContext appends
// after every host-registered Lifecycle whenever registry has at least one
// registered Job.
func newSchedulerLifecycle(registry *Registry, logger *slog.Logger, recorder observability.Recorder) Lifecycle {
	s := newScheduler(registry.jobs, newJobObserver(logger, recorder))
	return Lifecycle{
		Name:  schedulerLifecycleName,
		Start: s.Start,
		Stop:  s.Stop,
	}
}

// schedulerLifecycles returns the full Lifecycle list ServeContext should
// drive: every host-registered Lifecycle, in registration order, followed
// by the Scheduler lifecycle whenever registry has at least one registered
// Job. registry.Lifecycles() (and the slice backing it) is never mutated,
// so Registry.Lifecycles() never exposes the Scheduler lifecycle.
func schedulerLifecycles(registry *Registry) []Lifecycle {
	return schedulerLifecyclesWithObservability(registry, slog.Default(), observability.NopRecorder{})
}

func schedulerLifecyclesWithObservability(registry *Registry, logger *slog.Logger, recorder observability.Recorder) []Lifecycle {
	lifecycles := registry.Lifecycles()
	if len(registry.jobs) == 0 {
		return lifecycles
	}

	withScheduler := make([]Lifecycle, len(lifecycles)+1)
	copy(withScheduler, lifecycles)
	withScheduler[len(lifecycles)] = newSchedulerLifecycle(registry, logger, recorder)
	return withScheduler
}
