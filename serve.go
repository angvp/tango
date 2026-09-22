package tango

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/angvp/tango/db"
)

const defaultShutdownTimeout = 15 * time.Second

// serveConfig holds ServeContext's tunables, built from the ServeOption
// values passed to it.
type serveConfig struct {
	shutdownTimeout time.Duration
}

// ServeOption configures ServeContext.
type ServeOption func(*serveConfig)

// WithShutdownTimeout bounds each phase of ServeContext's graceful
// shutdown — draining in-flight HTTP requests, and running registered
// Lifecycle.Stop hooks — as two independent budgets, not one shared
// deadline. It defaults to 15 seconds. d must be positive.
func WithShutdownTimeout(d time.Duration) ServeOption {
	return func(c *serveConfig) {
		c.shutdownTimeout = d
	}
}

// ServeContext builds and runs config's registry the same way Serve does,
// but additionally starts and stops every registered Lifecycle around the
// HTTP server's own lifetime, and stops gracefully when ctx is canceled.
//
// Startup runs each Lifecycle.Start (skipping nil) in registration order.
// If a Start fails, or ctx is canceled before every Start has run, every
// already-started component (Start nil counts as trivially started) is
// stopped in reverse order and the HTTP server is never started.
//
// Once running, ctx cancellation drains the HTTP server (bounded by
// WithShutdownTimeout, forcing Server.Close if the drain deadline is hit),
// then stops every started Lifecycle in reverse order, each against its own
// fresh timeout-bounded context — a second, independent budget from the
// drain phase.
func ServeContext(ctx context.Context, config Config, sqlDB *sql.DB, dialect db.Dialect, opts ...ServeOption) error {
	sc := serveConfig{shutdownTimeout: defaultShutdownTimeout}
	for _, opt := range opts {
		opt(&sc)
	}
	if sc.shutdownTimeout <= 0 {
		return fmt.Errorf("tango: shutdown timeout must be positive, got %s", sc.shutdownTimeout)
	}

	registry, err := BuildRegistry(config)
	if err != nil {
		return fmt.Errorf("build registry: %w", err)
	}
	if err := registry.RunRegistration(); err != nil {
		return fmt.Errorf("run registration: %w", err)
	}
	registry.SetStore(db.NewStore(sqlDB, dialect))
	handler, err := registry.Routes().Handler()
	if err != nil {
		return fmt.Errorf("compile routes: %w", err)
	}

	// The Application context is never a direct child of ctx: its
	// cancellation is ServeContext's own decision (once draining
	// finishes), decoupled from the caller's cancellation signal.
	appCtx, cancelApp := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelApp()

	lifecycles := registry.Lifecycles()
	started := make([]Lifecycle, 0, len(lifecycles))
	for _, lc := range lifecycles {
		if err := ctx.Err(); err != nil {
			cancelApp()
			return rollbackStarted(started, err, sc.shutdownTimeout)
		}
		if lc.Start != nil {
			if err := lc.Start(appCtx); err != nil {
				cancelApp()
				return rollbackStarted(started, err, sc.shutdownTimeout)
			}
		}
		started = append(started, lc)
	}

	listener, err := net.Listen("tcp", config.Addr)
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
		errs = append(errs, stopLifecycles(started, sc.shutdownTimeout)...)
		return errors.Join(errs...)

	case serveErr := <-serveErrCh:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errs := finalizeShutdown(server, sc.shutdownTimeout)
			errs = append(errs, fmt.Errorf("serve: %w", serveErr))
			cancelApp()
			errs = append(errs, stopLifecycles(started, sc.shutdownTimeout)...)
			return errors.Join(errs...)
		}
		cancelApp()
		return errors.Join(stopLifecycles(started, sc.shutdownTimeout)...)
	}
}

// finalizeShutdown drains server against a fresh context bounded by
// timeout, force-closing it if that deadline is hit.
func finalizeShutdown(server *http.Server, timeout time.Duration) []error {
	drainCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shutdownErr := server.Shutdown(drainCtx)
	if shutdownErr == nil {
		return nil
	}
	if errors.Is(shutdownErr, context.DeadlineExceeded) {
		if closeErr := server.Close(); closeErr != nil {
			return []error{closeErr}
		}
		return nil
	}
	return []error{shutdownErr}
}

// stopLifecycles calls Stop (skipping nil) on every entry in started, in
// reverse order, each against its own fresh context bounded by timeout —
// an independent budget per call, not a shared remainder.
func stopLifecycles(started []Lifecycle, timeout time.Duration) []error {
	var errs []error
	for i := len(started) - 1; i >= 0; i-- {
		lc := started[i]
		if lc.Stop == nil {
			continue
		}
		stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
		err := lc.Stop(stopCtx)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("stop lifecycle %q: %w", lc.Name, err))
		}
	}
	return errs
}

// rollbackStarted joins triggerErr with the result of stopping every
// already-started component in reverse order — used both when a Start
// fails partway through and when the caller cancels ctx before startup
// finishes.
func rollbackStarted(started []Lifecycle, triggerErr error, timeout time.Duration) error {
	errs := append([]error{triggerErr}, stopLifecycles(started, timeout)...)
	return errors.Join(errs...)
}
