package tango

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/observability"
)

const defaultShutdownTimeout = 15 * time.Second

// newListener is the seam ServeContext binds through. Overridden in tests
// to inject a listener whose Accept can be made to fail on demand, so a
// genuine (non-http.ErrServerClosed) Server.Serve error can be exercised
// deterministically without a public testing API.
var newListener = net.Listen

// serveConfig holds ServeContext's tunables, built from the ServeOption
// values passed to it.
type serveConfig struct {
	shutdownTimeout time.Duration
	logger          *slog.Logger
	recorder        observability.Recorder
	recorderEnabled bool
}

// ServeOption configures ServeContext.
type ServeOption func(*serveConfig)

// WithShutdownTimeout bounds each phase of ServeContext's graceful
// shutdown — draining in-flight HTTP requests, and running every
// registered Lifecycle.Stop hook — as two independent budgets, not one
// shared deadline. It defaults to 15 seconds. d must be positive.
func WithShutdownTimeout(d time.Duration) ServeOption {
	return func(c *serveConfig) {
		c.shutdownTimeout = d
	}
}

// WithLogger configures framework-owned structured logging.
func WithLogger(logger *slog.Logger) ServeOption {
	return func(c *serveConfig) { c.logger = logger }
}

// WithRecorder configures automatic HTTP and scheduler metrics. A nil
// recorder is normalized to the no-op implementation.
func WithRecorder(recorder observability.Recorder) ServeOption {
	return func(c *serveConfig) {
		if recorder == nil {
			c.recorder = observability.NopRecorder{}
			c.recorderEnabled = false
			return
		}
		c.recorder = recorder
		_, c.recorderEnabled = recorder.(observability.NopRecorder)
		c.recorderEnabled = !c.recorderEnabled
	}
}

// ServeContext builds and runs config's registry the same way Serve does,
// but additionally starts and stops every registered Lifecycle around the
// HTTP server's own lifetime, and stops gracefully when ctx is canceled.
//
// Startup runs each Lifecycle.Start (skipping nil) in registration order,
// each against the long-lived Application context. If the caller cancels
// ctx while a Start is running, the Application context is canceled
// promptly so a cooperative Start can observe it and return; ServeContext
// always waits for that Start to actually finish before rolling back, so
// no startup goroutine is ever leaked. If a Start fails, or ctx is
// canceled before every Start has run, every already-started component
// (Start nil counts as trivially started; the interrupted/failed one does
// not) is stopped in reverse order and the HTTP server is never started.
//
// Once every component has started, ctx cancellation no longer touches the
// Application context directly — it stays live through HTTP draining
// (bounded by WithShutdownTimeout, forcing Server.Close if the drain
// deadline is hit) and is only canceled afterward, immediately before every
// started Lifecycle is stopped in reverse order against one shared,
// independently-timed context — a second budget from the drain phase, but
// one budget for the whole stop phase, not one per component.
func ServeContext(ctx context.Context, config Config, sqlDB *sql.DB, dialect db.Dialect, opts ...ServeOption) error {
	sc := serveConfig{shutdownTimeout: defaultShutdownTimeout, logger: slog.Default(), recorder: observability.NopRecorder{}}
	for _, opt := range opts {
		opt(&sc)
	}
	if sc.shutdownTimeout <= 0 {
		return fmt.Errorf("tango: shutdown timeout must be positive, got %s", sc.shutdownTimeout)
	}
	if sc.logger == nil {
		return fmt.Errorf("tango: logger must not be nil")
	}

	registry, err := BuildRegistry(config)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return fmt.Errorf("run registration: %w", err)
	}
	registry.SetStore(db.NewStore(sqlDB, dialect))
	registry.Routes().setObservability(sc.logger, sc.recorder, sc.recorderEnabled)
	handler, err := registry.Routes().Handler()
	if err != nil {
		return fmt.Errorf("compile routes: %w", err)
	}

	// The Application context is never a direct child of ctx: whether and
	// when its cancellation follows the caller's is ServeContext's own
	// decision — promptly while a Start is actively running (so a
	// cooperative Start can return), never during HTTP draining, and always
	// once draining finishes.
	appCtx, cancelApp := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelApp()

	lifecycles := schedulerLifecyclesWithObservability(registry, sc.logger, sc.recorder)
	started := make([]Lifecycle, 0, len(lifecycles))
	for _, lc := range lifecycles {
		if err := ctx.Err(); err != nil {
			cancelApp()
			return rollbackStarted(started, err, sc.shutdownTimeout)
		}
		if lc.Start != nil {
			startErrCh := make(chan error, 1)
			go func() { startErrCh <- lc.Start(appCtx) }()

			select {
			case err := <-startErrCh:
				if err != nil {
					cancelApp()
					return rollbackStarted(started, err, sc.shutdownTimeout)
				}
				// Start succeeded outright. If ctx happened to be canceled
				// at essentially the same instant, the top-of-loop check on
				// the next iteration (or the ctx.Done() branch right after
				// Serve starts, if this was the last component) catches it
				// and rolls back cleanly from there.

			case <-ctx.Done():
				// Cancel promptly so a Start cooperatively watching appCtx
				// can return, then always wait for it — never leave it
				// running unobserved (that would leak the startup goroutine
				// and risk calling Stop concurrently with a still-running
				// Start).
				cancelApp()
				trigger := ctx.Err()
				startErr := <-startErrCh
				if startErr != nil {
					// Start itself failed, or was interrupted and reports
					// that by returning a non-nil error (idiomatically its
					// own ctx.Err()): it does not count as successfully
					// started, so it's excluded from rollback and its Stop
					// never runs.
					return rollbackStarted(started, errors.Join(trigger, startErr), sc.shutdownTimeout)
				}
				// Start actually completed successfully — via a nil
				// return — despite racing with cancellation: it IS a
				// successfully started component, so its Stop must run
				// during rollback.
				started = append(started, lc)
				return rollbackStarted(started, trigger, sc.shutdownTimeout)
			}
		}
		started = append(started, lc)
	}

	listener, err := newListener("tcp", config.Addr)
	if err != nil {
		cancelApp()
		return rollbackStarted(started, fmt.Errorf("listen: %w", err), sc.shutdownTimeout)
	}

	server := &http.Server{Addr: config.Addr, Handler: handler}
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- server.Serve(listener) }()

	select {
	case <-ctx.Done():
		errs := finalizeShutdown(server, sc.shutdownTimeout)
		if serveErr := <-serveErrCh; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("serve: %w", serveErr))
		}
		cancelApp()
		errs = append(errs, stopStarted(started, sc.shutdownTimeout)...)
		return errors.Join(errs...)

	case serveErr := <-serveErrCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errs := finalizeShutdown(server, sc.shutdownTimeout)
			errs = append(errs, fmt.Errorf("serve: %w", serveErr))
			cancelApp()
			errs = append(errs, stopStarted(started, sc.shutdownTimeout)...)
			return errors.Join(errs...)
		}
		cancelApp()
		return errors.Join(stopStarted(started, sc.shutdownTimeout)...)
	}
}

// finalizeShutdown drains server against a fresh context bounded by
// timeout. If the drain deadline is hit, server is force-closed before
// returning — but the drain-timeout error is still reported (wrapped, so
// errors.Is(err, context.DeadlineExceeded) still finds it): shutdown was
// not fully graceful even though the force-close itself succeeded, since
// in-flight requests may have been aborted. If the force-close also fails,
// both errors are returned.
func finalizeShutdown(server *http.Server, timeout time.Duration) []error {
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shutdownErr := server.Shutdown(drainCtx)
	if shutdownErr == nil {
		return nil
	}
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		return []error{fmt.Errorf("drain HTTP server: %w", shutdownErr)}
	}

	errs := []error{fmt.Errorf("drain HTTP server: %w", shutdownErr)}
	if closeErr := server.Close(); closeErr != nil {
		errs = append(errs, fmt.Errorf("force-close HTTP server: %w", closeErr))
	}
	return errs
}

// stopStarted stops every entry in started, in reverse order, against one
// shared context bounded by timeout — a single budget for the whole
// reverse-order pass, not one fresh budget per component. A Stop hook that
// runs past the shared deadline doesn't reset or extend it for the hooks
// still to come; once the deadline is hit, remaining hooks still run,
// against that now-expired context, so they get a chance at immediate/
// best-effort cleanup rather than being skipped outright.
func stopStarted(started []Lifecycle, timeout time.Duration) []error {
	stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return stopLifecycles(stopCtx, started)
}

// stopLifecycles calls Stop (skipping nil) on every entry in started, in
// reverse order, all against the single ctx given — every caller is
// responsible for bounding ctx itself (see stopStarted/rollbackStarted).
func stopLifecycles(ctx context.Context, started []Lifecycle) []error {
	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		lc := started[i]
		if lc.Stop == nil {
			continue
		}
		if err := lc.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop lifecycle %q: %w", lc.Name, err))
		}
	}
	return errs
}

// rollbackStarted joins triggerErr with the result of stopping every
// already-started component in reverse order, all against one shared
// timeout-bounded context — used both when a Start fails partway through
// and when the caller cancels ctx before startup finishes.
func rollbackStarted(started []Lifecycle, triggerErr error, timeout time.Duration) error {
	errs := append([]error{triggerErr}, stopStarted(started, timeout)...)
	return errors.Join(errs...)
}
