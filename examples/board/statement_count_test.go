package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"
	"testing"

	"github.com/angvp/tango/db"
)

// statementCounter counts the statements database/sql sends to the driver,
// including transaction starts. It is built on the public database/sql and
// driver interfaces only, so the example can prove a statement count without
// importing the framework's internal test helpers.
type statementCounter struct{ n atomic.Int64 }

func (c *statementCounter) Count() int64 { return c.n.Load() }

// openCountingDB returns a fresh in-memory SQLite database whose statements
// are counted. It runs only on SQLite: the database under test is opened by
// testdb, which gives PostgreSQL and MySQL a private schema per test, and
// this wrapper has no way to reach that isolation from outside testdb.
func openCountingDB(t *testing.T) (*sql.DB, *statementCounter) {
	t.Helper()
	parsed, err := db.ParseDSN("sqlite://:memory:")
	if err != nil {
		t.Fatal(err)
	}
	probe, err := sql.Open(parsed.Driver, parsed.Source)
	if err != nil {
		t.Fatal(err)
	}
	drv := probe.Driver()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	counter := &statementCounter{}
	sqlDB := sql.OpenDB(countingConnector{
		inner:   dsnConnector{drv: drv, dsn: parsed.Source},
		counter: counter,
	})
	// Each connection to :memory: is its own empty database.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB, counter
}

type dsnConnector struct {
	drv driver.Driver
	dsn string
}

func (c dsnConnector) Connect(context.Context) (driver.Conn, error) { return c.drv.Open(c.dsn) }
func (c dsnConnector) Driver() driver.Driver                        { return c.drv }

type countingConnector struct {
	inner   driver.Connector
	counter *statementCounter
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &countingConn{Conn: conn, counter: c.counter}, nil
}

func (c countingConnector) Driver() driver.Driver { return c.inner.Driver() }

type countingConn struct {
	driver.Conn
	counter *statementCounter
}

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var (
		stmt driver.Stmt
		err  error
	)
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		stmt, err = pc.PrepareContext(ctx, query)
	} else {
		stmt, err = c.Conn.Prepare(query)
	}
	if err != nil {
		return nil, err
	}
	return &countingStmt{Stmt: stmt, counter: c.counter}, nil
}

// QueryContext and ExecContext return driver.ErrSkip when the inner
// connection does not implement the fast path, so database/sql falls back to
// Prepare, and the statement is counted once on that path instead.
func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.counter.n.Add(1)
	return qc.QueryContext(ctx, query, args)
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.counter.n.Add(1)
	return ec.ExecContext(ctx, query, args)
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.counter.n.Add(1)
	if cb, ok := c.Conn.(driver.ConnBeginTx); ok {
		return cb.BeginTx(ctx, opts)
	}
	return c.Conn.Begin()
}

func (c *countingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if nc, ok := c.Conn.(driver.NamedValueChecker); ok {
		return nc.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

type countingStmt struct {
	driver.Stmt
	counter *statementCounter
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.counter.n.Add(1)
	if sc, ok := s.Stmt.(driver.StmtExecContext); ok {
		return sc.ExecContext(ctx, args)
	}
	return s.Stmt.Exec(namedValues(args))
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.counter.n.Add(1)
	if sc, ok := s.Stmt.(driver.StmtQueryContext); ok {
		return sc.QueryContext(ctx, args)
	}
	return s.Stmt.Query(namedValues(args))
}

func namedValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return values
}

// TestStatementCounterSeesPerRowQueries is the known-positive control for the
// counter: a loop that issues one lookup per row must register one statement
// per row, so a count that stays low elsewhere is a real result.
func TestStatementCounterSeesPerRowQueries(t *testing.T) {
	sqlDB, counter := openCountingDB(t)
	if _, err := sqlDB.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO probe (id, v) VALUES (1, 'a'), (2, 'b'), (3, 'c')`); err != nil {
		t.Fatal(err)
	}
	before := counter.Count()
	for id := 1; id <= 3; id++ {
		var v string
		if err := sqlDB.QueryRow(`SELECT v FROM probe WHERE id = ?`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
	}
	if got := counter.Count() - before; got != 3 {
		t.Fatalf("per-row loop over 3 rows counted %d statements, want 3", got)
	}
}
