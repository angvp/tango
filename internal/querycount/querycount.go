// Package querycount counts the statements a test's database sends to the
// driver. It wraps the database testdb opens for the test on any of the three
// test dialects, so a count is taken at the same point on each. It is test-only
// and internal: production code and hosts never see it.
//
// A zero from a Counter is a measurement only beside a known positive from the
// same Counter, so tests pair every count with a run that must record statements.
package querycount

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"

	"github.com/angvp/tango/internal/testdbhook"
)

// Counter records each statement that reaches the driver, in order. Statements
// sent through a transaction count the same as any other.
type Counter struct {
	mu         sync.Mutex
	statements []string
	events     []Event
	failures   []injectedFailure
}

// Kind says what an Event records.
type Kind string

const (
	// KindStatement is a statement that reached the driver.
	KindStatement Kind = "statement"
	// KindBegin is a transaction the driver began.
	KindBegin Kind = "begin"
	// KindCommit is a transaction the driver was asked to commit.
	KindCommit Kind = "commit"
	// KindRollback is a transaction the driver was asked to roll back.
	KindRollback Kind = "rollback"
)

// Event is one entry in the order the driver saw it. A statement event
// carries its SQL text. A begin event carries the isolation level and the
// read-only flag the transaction was opened with; the other kinds carry
// neither.
type Event struct {
	Kind      Kind
	Statement string
	Isolation sql.IsolationLevel
	ReadOnly  bool
}

type injectedFailure struct {
	fragment string
	err      error
	skip     int  // matching statements to let through before this failure fires
	once     bool // the failure is spent after it fires once
	spent    bool
}

// Attach routes the database testdb opens for t through a recording driver
// and returns the Counter that records it. Statements are recorded from the
// moment testdb opens the database, so call Reset after any setup the test
// does not want counted.
func Attach(t testing.TB) *Counter {
	t.Helper()
	counter := &Counter{}
	testdbhook.Attach(t, func(inner driver.Connector) driver.Connector {
		return countingConnector{inner: inner, counter: counter}
	})
	return counter
}

// Reset discards the statements recorded so far.
func (c *Counter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = nil
	c.events = nil
}

// Events returns every statement and transaction boundary recorded so far, in
// the order the driver saw them. Statements is the statement subset of it.
func (c *Counter) Events() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Event(nil), c.events...)
}

// Statements returns the SQL text of each recorded statement, in order.
func (c *Counter) Statements() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.statements...)
}

// Matching returns how many recorded statements contain fragment as a literal
// substring. It does not interpret fragment as a pattern.
func (c *Counter) Matching(fragment string) int {
	n := 0
	for _, statement := range c.Statements() {
		if strings.Contains(statement, fragment) {
			n++
		}
	}
	return n
}

// FailMatching makes each later statement that contains fragment fail with err
// before it reaches the driver, and does not record it. It exists so a test can
// drive an error path that the database would not produce on its own.
func (c *Counter) FailMatching(fragment string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = append(c.failures, injectedFailure{fragment: fragment, err: err})
}

// FailNthMatching makes the nth later statement (1-based) that contains
// fragment fail with err before it reaches the driver, and lets every other
// statement through. It exists so a test can fail a later chunk of a read
// whose earlier chunks must succeed.
func (c *Counter) FailNthMatching(fragment string, n int, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = append(c.failures, injectedFailure{fragment: fragment, err: err, skip: max(n-1, 0), once: true})
}

func (c *Counter) record(query string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statements = append(c.statements, query)
	c.events = append(c.events, Event{Kind: KindStatement, Statement: query})
}

func (c *Counter) recordEvent(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

// injected returns the error armed for query, or nil when none matches.
func (c *Counter) injected(query string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.failures {
		f := &c.failures[i]
		if f.spent || !strings.Contains(query, f.fragment) {
			continue
		}
		if f.skip > 0 {
			f.skip--
			continue
		}
		if f.once {
			f.spent = true
		}
		return f.err
	}
	return nil
}

// countingConnector opens connections that record every statement.
type countingConnector struct {
	inner   driver.Connector
	counter *Counter
}

func (c countingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &countingConn{inner: conn, counter: c.counter}, nil
}

func (c countingConnector) Driver() driver.Driver { return c.inner.Driver() }

// countingConn forwards each optional driver interface to the inner connection
// and returns driver.ErrSkip when the inner one lacks it, so database/sql falls
// back exactly as it would without the wrapper. A statement is recorded only
// when it reaches the inner driver, never when the driver declines it with
// ErrSkip, so a fallback path is not counted twice.
type countingConn struct {
	inner    driver.Conn
	counter  *Counter
	declined bool // the inner driver declined the last Exec or Query with ErrSkip, so the prepared fallback follows
}

// takeDeclined reports whether the last statement was declined at connection
// level and clears the flag, so the prepared fallback can inherit its decision.
func (c *countingConn) takeDeclined() bool {
	declined := c.declined
	c.declined = false
	return declined
}

func (c *countingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{inner: stmt, query: query, counter: c.counter, decided: c.takeDeclined()}, nil
}

func (c *countingConn) Close() error { return c.inner.Close() }

func (c *countingConn) Begin() (driver.Tx, error) { //nolint:staticcheck // database/sql calls BeginTx when present
	tx, err := c.inner.Begin() //nolint:staticcheck // database/sql calls BeginTx when present
	return c.begun(tx, err, driver.TxOptions{})
}

func (c *countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	bc, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	tx, err := bc.BeginTx(ctx, opts)
	return c.begun(tx, err, opts)
}

// begun records a transaction the driver began and wraps it so its commit and
// rollback are recorded in order with the statements.
func (c *countingConn) begun(tx driver.Tx, err error, opts driver.TxOptions) (driver.Tx, error) {
	if err != nil {
		return nil, err
	}
	c.counter.recordEvent(Event{Kind: KindBegin, Isolation: sql.IsolationLevel(opts.Isolation), ReadOnly: opts.ReadOnly})
	return &countingTx{inner: tx, counter: c.counter}, nil
}

// countingTx records the commit or rollback it forwards.
type countingTx struct {
	inner   driver.Tx
	counter *Counter
}

func (t *countingTx) Commit() error {
	t.counter.recordEvent(Event{Kind: KindCommit})
	return t.inner.Commit()
}

func (t *countingTx) Rollback() error {
	t.counter.recordEvent(Event{Kind: KindRollback})
	return t.inner.Rollback()
}

func (c *countingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	pc, ok := c.inner.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	stmt, err := pc.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	return &countingStmt{inner: stmt, query: query, counter: c.counter, decided: c.takeDeclined()}, nil
}

func (c *countingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.declined = false
	if err := c.counter.injected(query); err != nil {
		return nil, err
	}
	result, err := ec.ExecContext(ctx, query, args)
	if err == driver.ErrSkip {
		c.declined = true
		return nil, err
	}
	c.counter.record(query)
	return result, err
}

func (c *countingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.declined = false
	if err := c.counter.injected(query); err != nil {
		return nil, err
	}
	rows, err := qc.QueryContext(ctx, query, args)
	if err == driver.ErrSkip {
		c.declined = true
		return nil, err
	}
	c.counter.record(query)
	return rows, err
}

// Ping and ResetSession always report success when the inner connection lacks
// them, and IsValid reports true, which is what database/sql assumes of a
// connection that does not implement these optional interfaces.
func (c *countingConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *countingConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *countingConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *countingConn) CheckNamedValue(nv *driver.NamedValue) error {
	if checker, ok := c.inner.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// countingStmt records each execution of a prepared statement.
type countingStmt struct {
	inner   driver.Stmt
	query   string
	counter *Counter
	decided bool // the connection already applied injection to this statement
}

// inject applies the armed failures to one execution. A statement the
// connection declined with ErrSkip was decided there, so its prepared fallback
// must not decide it again: one statement consumes one armed skip.
func (s *countingStmt) inject() error {
	if s.decided {
		s.decided = false
		return nil
	}
	return s.counter.injected(s.query)
}

func (s *countingStmt) Close() error  { return s.inner.Close() }
func (s *countingStmt) NumInput() int { return s.inner.NumInput() }

func (s *countingStmt) Exec(args []driver.Value) (driver.Result, error) { //nolint:staticcheck // legacy path, reached only without ExecContext
	if err := s.inject(); err != nil {
		return nil, err
	}
	s.counter.record(s.query)
	return s.inner.Exec(args) //nolint:staticcheck // legacy path, reached only without ExecContext
}

func (s *countingStmt) Query(args []driver.Value) (driver.Rows, error) { //nolint:staticcheck // legacy path, reached only without QueryContext
	if err := s.inject(); err != nil {
		return nil, err
	}
	s.counter.record(s.query)
	return s.inner.Query(args) //nolint:staticcheck // legacy path, reached only without QueryContext
}

func (s *countingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	sc, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := s.inject(); err != nil {
		return nil, err
	}
	result, err := sc.ExecContext(ctx, args)
	if err == driver.ErrSkip {
		return nil, err
	}
	s.counter.record(s.query)
	return result, err
}

func (s *countingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	sc, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if err := s.inject(); err != nil {
		return nil, err
	}
	rows, err := sc.QueryContext(ctx, args)
	if err == driver.ErrSkip {
		return nil, err
	}
	s.counter.record(s.query)
	return rows, err
}
