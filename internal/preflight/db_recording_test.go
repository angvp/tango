package preflight

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"
)

// dbCallLog records every database/sql-level interaction that reaches the
// driver. A zero count is only a measurement when a known-positive run through
// the same log records calls, which the positive controls assert.
type dbCallLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *dbCallLog) record(kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, kind)
}

func (l *dbCallLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func (l *dbCallLog) has(kind string) bool {
	for _, c := range l.snapshot() {
		if c == kind {
			return true
		}
	}
	return false
}

// newRecordingDB returns a SQLite in-memory database whose every driver-level
// call is logged. The log is snapshotted by the caller before any cleanup, so
// the Close issued at test end is never counted against the assertion.
func newRecordingDB(t *testing.T) (*sql.DB, *dbCallLog) {
	t.Helper()
	probe, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open sqlite: %v", err)
	}
	inner := probe.Driver()
	if err := probe.Close(); err != nil {
		t.Fatalf("closing probe handle: %v", err)
	}

	log := &dbCallLog{}
	sqlDB := sql.OpenDB(loggingConnector{drv: loggingDriver{inner: inner, log: log}, dsn: ":memory:"})
	// One connection keeps the in-memory database alive across statements.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB, log
}

type loggingConnector struct {
	drv loggingDriver
	dsn string
}

func (c loggingConnector) Connect(context.Context) (driver.Conn, error) {
	return c.drv.Open(c.dsn)
}

func (c loggingConnector) Driver() driver.Driver { return c.drv }

type loggingDriver struct {
	inner driver.Driver
	log   *dbCallLog
}

func (d loggingDriver) Open(name string) (driver.Conn, error) {
	d.log.record("connect")
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &loggingConn{inner: conn, log: d.log}, nil
}

// loggingConn forwards each optional interface to the inner connection and
// returns driver.ErrSkip when the inner one lacks it, so database/sql falls
// back to Prepare and the fallback itself is recorded.
type loggingConn struct {
	inner driver.Conn
	log   *dbCallLog
}

func (c *loggingConn) Prepare(query string) (driver.Stmt, error) {
	c.log.record("prepare")
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &loggingStmt{inner: stmt, log: c.log}, nil
}

func (c *loggingConn) Close() error {
	c.log.record("close")
	return c.inner.Close()
}

func (c *loggingConn) Begin() (driver.Tx, error) {
	c.log.record("begin")
	tx, err := c.inner.Begin() //nolint:staticcheck // database/sql calls BeginTx when present; this is the fallback
	if err != nil {
		return nil, err
	}
	return &loggingTx{inner: tx, log: c.log}, nil
}

func (c *loggingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	pc, ok := c.inner.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.record("prepare")
	stmt, err := pc.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &loggingStmt{inner: stmt, log: c.log}, nil
}

func (c *loggingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	bc, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.record("begin")
	tx, err := bc.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &loggingTx{inner: tx, log: c.log}, nil
}

func (c *loggingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.record("exec")
	return ec.ExecContext(ctx, query, args)
}

func (c *loggingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.log.record("query")
	return qc.QueryContext(ctx, query, args)
}

func (c *loggingConn) Ping(ctx context.Context) error {
	c.log.record("ping")
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

type loggingStmt struct {
	inner driver.Stmt
	log   *dbCallLog
}

func (s *loggingStmt) Close() error {
	s.log.record("stmt-close")
	return s.inner.Close()
}

func (s *loggingStmt) NumInput() int { return s.inner.NumInput() }

func (s *loggingStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.log.record("stmt-exec")
	return s.inner.Exec(args) //nolint:staticcheck // legacy path, reached only without ExecContext
}

func (s *loggingStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.log.record("stmt-query")
	return s.inner.Query(args) //nolint:staticcheck // legacy path, reached only without QueryContext
}

func (s *loggingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	sc, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.log.record("stmt-exec")
	return sc.ExecContext(ctx, args)
}

func (s *loggingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	sc, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.log.record("stmt-query")
	return sc.QueryContext(ctx, args)
}

type loggingTx struct {
	inner driver.Tx
	log   *dbCallLog
}

func (t *loggingTx) Commit() error {
	t.log.record("commit")
	return t.inner.Commit()
}

func (t *loggingTx) Rollback() error {
	t.log.record("rollback")
	return t.inner.Rollback()
}

// duplicateNameMigrations registers one exact (App, Name) twice, the second
// conflict shape Validate reports alongside the shared-number shape.
func duplicateNameMigrations() []migration.Migration {
	first := validMigrations()[0]
	return []migration.Migration{first, first}
}

// Positive control for the -check and -migrate refusals: the same recording
// database, given a valid registry through the real apply path, must log the
// driver calls. Without this, a zero in the refusal tests means nothing.
func TestRecordingDBPositiveControlAppliesValidRegistry(t *testing.T) {
	sqlDB, log := newRecordingDB(t)

	if err := migration.ApplyPending(context.Background(), sqlDB, db.SQLite, validMigrations()); err != nil {
		t.Fatalf("ApplyPending on a valid registry: %v", err)
	}
	if !log.has("connect") {
		t.Fatalf("calls = %v, want a connect recorded", log.snapshot())
	}
	if !log.has("exec") && !log.has("stmt-exec") {
		t.Fatalf("calls = %v, want an exec recorded for CREATE TABLE and tracking", log.snapshot())
	}
}
