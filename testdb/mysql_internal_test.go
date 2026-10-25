package testdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// fakeConn implements every optional interface rebindConn forwards to, and
// records what reached it. It lives in this package (not testdb_test) because
// rebindConn and openMySQL are unexported.
type fakeConn struct {
	query string
	args  []driver.NamedValue
	calls []string

	prepareErr, execErr, queryErr, beginErr, pingErr, resetErr, checkErr error
	valid                                                                bool
	beganOpts                                                            driver.TxOptions
}

func (c *fakeConn) record(name, query string, args []driver.NamedValue) {
	c.calls = append(c.calls, name)
	c.query = query
	c.args = args
}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *fakeConn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	c.calls = append(c.calls, "prepare")
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	return &fakeStmt{conn: c, query: query}, nil
}

func (c *fakeConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.calls = append(c.calls, "begin")
	c.beganOpts = opts
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return fakeTx{}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.record("exec", query, args)
	if c.execErr != nil {
		return nil, c.execErr
	}
	return driver.RowsAffected(7), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.record("query", query, args)
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	return fakeRows{}, nil
}

func (c *fakeConn) Ping(context.Context) error {
	c.calls = append(c.calls, "ping")
	return c.pingErr
}

func (c *fakeConn) ResetSession(context.Context) error {
	c.calls = append(c.calls, "reset")
	return c.resetErr
}

func (c *fakeConn) IsValid() bool {
	c.calls = append(c.calls, "valid")
	return c.valid
}

func (c *fakeConn) CheckNamedValue(nv *driver.NamedValue) error {
	c.calls = append(c.calls, "check")
	return c.checkErr
}

// fakeStmt records the statement's own executions; the query it was prepared
// with is kept so a test can tell the rewritten text apart from the original.
type fakeStmt struct {
	conn  *fakeConn
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return 2 }

func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("fakeStmt: Exec without context is not expected")
}

func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("fakeStmt: Query without context is not expected")
}

func (s *fakeStmt) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	s.conn.record("stmt-exec", s.query, args)
	return driver.RowsAffected(3), nil
}

func (s *fakeStmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	s.conn.record("stmt-query", s.query, args)
	return fakeRows{}, nil
}

// fakeRows is an empty result set.
type fakeRows struct{}

func (fakeRows) Columns() []string              { return nil }
func (fakeRows) Close() error                   { return nil }
func (fakeRows) Next(dest []driver.Value) error { return io.EOF }

type fakeTx struct{}

func (fakeTx) Commit() error   { return nil }
func (fakeTx) Rollback() error { return nil }

// fakeDriver and fakeConnector let database/sql drive the wrapper end to end.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fakeDriver: Open is not expected")
}

type fakeConnector struct {
	conn       *fakeConn
	connectErr error
	drv        driver.Driver
}

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	if c.connectErr != nil {
		return nil, c.connectErr
	}
	return c.conn, nil
}

func (c fakeConnector) Driver() driver.Driver { return c.drv }

// fakeTB mirrors the fake in testdb_test.go, which this package cannot import.
type fakeTB struct {
	testing.TB
	failed   bool
	messages []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func runFake(t *testing.T, fn func(tb testing.TB)) *fakeTB {
	t.Helper()
	fake := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(fake)
	}()
	<-done
	return fake
}

func namedValues(values ...any) []driver.NamedValue {
	out := make([]driver.NamedValue, len(values))
	for i, v := range values {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

func values(args []driver.NamedValue) []any {
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out
}

func ordinals(args []driver.NamedValue) []int {
	out := make([]int, len(args))
	for i, a := range args {
		out[i] = a.Ordinal
	}
	return out
}

func TestRebindRewritesNumberedPlaceholdersAndReordersArgs(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		args      []driver.NamedValue
		wantQuery string
		wantArgs  []any
		wantOrds  []int
	}{
		{
			name:      "reused placeholder duplicates its argument",
			query:     "SELECT $2, $1, $2",
			args:      namedValues("a", "b"),
			wantQuery: "SELECT ?, ?, ?",
			wantArgs:  []any{"b", "a", "b"},
			wantOrds:  []int{1, 2, 3},
		},
		{
			name:      "placeholders out of order are reordered",
			query:     "UPDATE t SET x = $3 WHERE id = $1 AND y = $2",
			args:      namedValues(1, 2, 3),
			wantQuery: "UPDATE t SET x = ? WHERE id = ? AND y = ?",
			wantArgs:  []any{3, 1, 2},
			wantOrds:  []int{1, 2, 3},
		},
		{
			name:      "placeholder inside a quoted literal is not rewritten",
			query:     "SELECT '$1', $1",
			args:      namedValues("a"),
			wantQuery: "SELECT '$1', ?",
			wantArgs:  []any{"a"},
			wantOrds:  []int{1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &fakeConn{}
			rc := &rebindConn{conn}

			if _, err := rc.ExecContext(context.Background(), test.query, test.args); err != nil {
				t.Fatalf("ExecContext: %v", err)
			}

			if conn.query != test.wantQuery {
				t.Errorf("query = %q, want %q", conn.query, test.wantQuery)
			}
			if got := values(conn.args); !reflect.DeepEqual(got, test.wantArgs) {
				t.Errorf("args = %v, want %v", got, test.wantArgs)
			}
			if got := ordinals(conn.args); !reflect.DeepEqual(got, test.wantOrds) {
				t.Errorf("ordinals = %v, want %v", got, test.wantOrds)
			}
		})
	}
}

func TestRebindPassesQueriesWithoutNumberedPlaceholdersThrough(t *testing.T) {
	args := namedValues("a", "b")
	conn := &fakeConn{}
	rc := &rebindConn{conn}

	if _, err := rc.QueryContext(context.Background(), "SELECT 1", args); err != nil {
		t.Fatalf("QueryContext: %v", err)
	}

	if conn.query != "SELECT 1" {
		t.Errorf("query = %q, want unchanged %q", conn.query, "SELECT 1")
	}
	if !reflect.DeepEqual(conn.args, args) {
		t.Errorf("args = %v, want the caller's args unchanged %v", conn.args, args)
	}
}

func TestRebindPrepareContextWrapsOnlyWhenPlaceholdersWereRewritten(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantWrap  bool
		wantQuery string
	}{
		{"no placeholders returns the plain statement", "SELECT 1", false, "SELECT 1"},
		{"numbered placeholders return a reordering statement", "SELECT $2, $1", true, "SELECT ?, ?"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &fakeConn{}
			stmt, err := (&rebindConn{conn}).PrepareContext(context.Background(), test.query)
			if err != nil {
				t.Fatalf("PrepareContext: %v", err)
			}

			_, wrapped := stmt.(*rebindStmt)
			if wrapped != test.wantWrap {
				t.Fatalf("wrapped = %v, want %v (type %T)", wrapped, test.wantWrap, stmt)
			}
			inner := stmt
			if wrapped {
				inner = stmt.(*rebindStmt).Stmt
			}
			if got := inner.(*fakeStmt).query; got != test.wantQuery {
				t.Errorf("prepared query = %q, want %q", got, test.wantQuery)
			}
		})
	}
}

func TestRebindStmtReordersArgsForExecAndQuery(t *testing.T) {
	conn := &fakeConn{}
	stmt, err := (&rebindConn{conn}).PrepareContext(context.Background(), "SELECT $2, $1, $2")
	if err != nil {
		t.Fatalf("PrepareContext: %v", err)
	}
	rs, ok := stmt.(*rebindStmt)
	if !ok {
		t.Fatalf("PrepareContext returned %T, want *rebindStmt", stmt)
	}
	if got := rs.NumInput(); got != -1 {
		t.Errorf("NumInput() = %d, want -1", got)
	}

	ctx := context.Background()
	res, err := rs.ExecContext(ctx, namedValues("a", "b"))
	if err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 3 {
		t.Errorf("RowsAffected = %d, want 3 from the inner statement", n)
	}
	if conn.calls[len(conn.calls)-1] != "stmt-exec" || conn.query != "SELECT ?, ?, ?" {
		t.Errorf("exec reached %v with query %q, want stmt-exec on the rewritten text", conn.calls, conn.query)
	}
	if got := values(conn.args); !reflect.DeepEqual(got, []any{"b", "a", "b"}) {
		t.Errorf("exec args = %v, want [b a b]", got)
	}

	if _, err := rs.QueryContext(ctx, namedValues("a", "b")); err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	if conn.calls[len(conn.calls)-1] != "stmt-query" {
		t.Errorf("query reached %v, want stmt-query last", conn.calls)
	}
	if got := values(conn.args); !reflect.DeepEqual(got, []any{"b", "a", "b"}) {
		t.Errorf("query args = %v, want [b a b]", got)
	}
}

func TestRebindPropagatesInnerErrorsUnchanged(t *testing.T) {
	innerErr := errors.New("inner failure")
	tests := []struct {
		name string
		conn *fakeConn
		call func(rc *rebindConn) error
	}{
		{"prepare", &fakeConn{prepareErr: innerErr}, func(rc *rebindConn) error {
			_, err := rc.PrepareContext(context.Background(), "SELECT $1")
			return err
		}},
		{"exec", &fakeConn{execErr: innerErr}, func(rc *rebindConn) error {
			_, err := rc.ExecContext(context.Background(), "SELECT $1", namedValues("a"))
			return err
		}},
		{"query", &fakeConn{queryErr: innerErr}, func(rc *rebindConn) error {
			_, err := rc.QueryContext(context.Background(), "SELECT $1", namedValues("a"))
			return err
		}},
		{"begin", &fakeConn{beginErr: innerErr}, func(rc *rebindConn) error {
			_, err := rc.BeginTx(context.Background(), driver.TxOptions{})
			return err
		}},
		{"ping", &fakeConn{pingErr: innerErr}, func(rc *rebindConn) error {
			return rc.Ping(context.Background())
		}},
		{"reset", &fakeConn{resetErr: innerErr}, func(rc *rebindConn) error {
			return rc.ResetSession(context.Background())
		}},
		{"check", &fakeConn{checkErr: innerErr}, func(rc *rebindConn) error {
			return rc.CheckNamedValue(&driver.NamedValue{Value: "a"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(&rebindConn{test.conn}); err != innerErr {
				t.Fatalf("err = %v, want the inner error unchanged", err)
			}
		})
	}
}

func TestRebindDelegatesOptionalInterfaces(t *testing.T) {
	ctx := context.Background()

	t.Run("BeginTx passes options and returns the inner transaction", func(t *testing.T) {
		conn := &fakeConn{}
		opts := driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelSerializable), ReadOnly: true}
		tx, err := (&rebindConn{conn}).BeginTx(ctx, opts)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		if _, ok := tx.(fakeTx); !ok {
			t.Fatalf("BeginTx returned %T, want the inner fakeTx", tx)
		}
		if conn.beganOpts != opts {
			t.Errorf("inner BeginTx got %+v, want %+v", conn.beganOpts, opts)
		}
	})

	t.Run("Ping succeeds", func(t *testing.T) {
		conn := &fakeConn{}
		if err := (&rebindConn{conn}).Ping(ctx); err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if conn.calls[len(conn.calls)-1] != "ping" {
			t.Errorf("calls = %v, want ping", conn.calls)
		}
	})

	t.Run("ResetSession succeeds", func(t *testing.T) {
		conn := &fakeConn{}
		if err := (&rebindConn{conn}).ResetSession(ctx); err != nil {
			t.Fatalf("ResetSession: %v", err)
		}
		if conn.calls[len(conn.calls)-1] != "reset" {
			t.Errorf("calls = %v, want reset", conn.calls)
		}
	})

	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprintf("IsValid reports %v", valid), func(t *testing.T) {
			conn := &fakeConn{valid: valid}
			if got := (&rebindConn{conn}).IsValid(); got != valid {
				t.Fatalf("IsValid() = %v, want %v", got, valid)
			}
		})
	}

	t.Run("CheckNamedValue succeeds and reaches the inner conn", func(t *testing.T) {
		conn := &fakeConn{}
		nv := &driver.NamedValue{Value: "a"}
		if err := (&rebindConn{conn}).CheckNamedValue(nv); err != nil {
			t.Fatalf("CheckNamedValue: %v", err)
		}
		if conn.calls[len(conn.calls)-1] != "check" || nv.Value != "a" {
			t.Errorf("calls = %v, value = %v; want check reached with the value unchanged", conn.calls, nv.Value)
		}
	})
}

func TestRebindConnectorConnectWrapsTheInnerConn(t *testing.T) {
	inner := &fakeConn{}
	conn, err := (rebindConnector{fakeConnector{conn: inner}}).Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	rc, ok := conn.(*rebindConn)
	if !ok {
		t.Fatalf("Connect returned %T, want *rebindConn", conn)
	}
	if rc.Conn != driver.Conn(inner) {
		t.Errorf("wrapped conn = %v, want the inner connection", rc.Conn)
	}
}

func TestRebindConnectorConnectPropagatesInnerError(t *testing.T) {
	innerErr := errors.New("connect failure")
	conn, err := (rebindConnector{fakeConnector{connectErr: innerErr}}).Connect(context.Background())
	if err != innerErr {
		t.Fatalf("Connect err = %v, want the inner error unchanged", err)
	}
	if conn != nil {
		t.Fatalf("Connect returned conn %v alongside an error", conn)
	}
}

func TestRebindConnectorDriverReturnsTheInnerDriver(t *testing.T) {
	drv := fakeDriver{}
	got := rebindConnector{fakeConnector{drv: drv}}.Driver()
	if got != driver.Driver(drv) {
		t.Fatalf("Driver() = %v, want the inner connector's driver", got)
	}
}

// hasCall reports whether the inner conn saw a call by name. database/sql adds its
// own valid and check calls, so ordering is not asserted here.
func hasCall(calls []string, name string) bool {
	for _, c := range calls {
		if c == name {
			return true
		}
	}
	return false
}

func TestRebindThroughDatabaseSQL(t *testing.T) {
	conn := &fakeConn{}
	db := sql.OpenDB(rebindConnector{fakeConnector{conn: conn}})
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("PingContext: %v", err)
	}
	if !hasCall(conn.calls, "ping") {
		t.Errorf("Ping reached calls %v, want ping", conn.calls)
	}

	if _, err := db.ExecContext(ctx, "SELECT $2, $1, $2", "a", "b"); err != nil {
		t.Fatalf("ExecContext: %v", err)
	}
	if conn.query != "SELECT ?, ?, ?" {
		t.Errorf("Exec query = %q, want %q", conn.query, "SELECT ?, ?, ?")
	}
	if got := values(conn.args); !reflect.DeepEqual(got, []any{"b", "a", "b"}) {
		t.Errorf("Exec args = %v, want [b a b]", got)
	}

	rows, err := db.QueryContext(ctx, "SELECT $2, $1", "a", "b")
	if err != nil {
		t.Fatalf("QueryContext: %v", err)
	}
	_ = rows.Close()
	if conn.query != "SELECT ?, ?" {
		t.Errorf("Query query = %q, want %q", conn.query, "SELECT ?, ?")
	}
	if got := values(conn.args); !reflect.DeepEqual(got, []any{"b", "a"}) {
		t.Errorf("Query args = %v, want [b a]", got)
	}

	stmt, err := db.PrepareContext(ctx, "SELECT $2, $1, $2")
	if err != nil {
		t.Fatalf("PrepareContext: %v", err)
	}
	defer func() { _ = stmt.Close() }()
	if _, err := stmt.ExecContext(ctx, "a", "b"); err != nil {
		t.Fatalf("stmt.ExecContext: %v", err)
	}
	if !hasCall(conn.calls, "stmt-exec") {
		t.Errorf("prepared exec reached %v, want stmt-exec", conn.calls)
	}
	if got := values(conn.args); !reflect.DeepEqual(got, []any{"b", "a", "b"}) {
		t.Errorf("prepared exec args = %v, want [b a b]", got)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !hasCall(conn.calls, "begin") {
		t.Errorf("BeginTx reached calls %v, want begin", conn.calls)
	}
}

func TestOpenMySQLRejectsANonMySQLDSN(t *testing.T) {
	// No "/" before the database name, so mysql.ParseDSN rejects it without
	// contacting any server.
	const source = "root:secret@tcp(127.0.0.1:1)"

	fake := runFake(t, func(tb testing.TB) { openMySQL(tb, source) })

	if !fake.failed {
		t.Fatal("openMySQL with a non-MySQL DSN did not fail the test")
	}
	message := strings.Join(fake.messages, "\n")
	if !strings.Contains(message, "not a MySQL DSN") {
		t.Errorf("failure message %q does not say %q", message, "not a MySQL DSN")
	}
	if strings.Contains(message, "secret") {
		t.Errorf("failure message %q echoes the DSN's password", message)
	}
}
