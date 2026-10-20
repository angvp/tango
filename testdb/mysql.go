package testdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/sqlbind"
	"github.com/go-sql-driver/mysql"
)

// openMySQL creates a database only this test uses, drops it when the test
// ends, and returns a handle whose every connection uses it. Raw SQL written
// with numbered placeholders ($1, $2, …) is rewritten to MySQL's "?" on the
// way to the server, so a test's SQL does not depend on the dialect.
func openMySQL(t testing.TB, source string) *sql.DB {
	t.Helper()
	config, err := mysql.ParseDSN(source)
	if err != nil {
		t.Fatalf("testdb: %s: not a MySQL DSN", envVar)
	}

	server := config.Clone()
	server.DBName = ""
	admin, err := sql.Open("mysql", server.FormatDSN())
	if err != nil {
		t.Fatalf("testdb: open mysql: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("testdb: %s names a MySQL server that cannot be reached: %v", envVar, err)
	}

	name := newSchemaName(t)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		t.Fatalf("testdb: create database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE `"+name+"`"); err != nil {
			t.Errorf("testdb: drop database %s: %v", name, err)
		}
	})

	scoped := config.Clone()
	scoped.DBName = name
	connector, err := mysql.NewConnector(scoped)
	if err != nil {
		t.Fatalf("testdb: mysql connector: %v", err)
	}
	sqlDB := sql.OpenDB(rebindConnector{connector})
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// MySQLOnly skips the test unless the run's Test dialect is MySQL. The
// reason says what the test needs that SQLite and PostgreSQL cannot give it.
func MySQLOnly(t testing.TB, reason string) {
	t.Helper()
	if parseRunDSN(t).Dialect != db.MySQL {
		t.Skipf("testdb: MySQL only: %s", reason)
	}
}

// NotMySQL skips the test when the run's Test dialect is MySQL. The reason
// says what the test needs that MySQL cannot give it.
func NotMySQL(t testing.TB, reason string) {
	t.Helper()
	if parseRunDSN(t).Dialect == db.MySQL {
		t.Skipf("testdb: not on MySQL: %s", reason)
	}
}

// rebindConnector wraps a MySQL connector so every statement sent through it
// has its numbered placeholders rewritten.
type rebindConnector struct{ inner driver.Connector }

func (c rebindConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rebindConn{conn}, nil
}

func (c rebindConnector) Driver() driver.Driver { return c.inner.Driver() }

// rebindConn forwards every optional interface the MySQL connection has, with
// the query (and argument order) rewritten where one is passed.
type rebindConn struct{ driver.Conn }

func (c *rebindConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	rewritten, order := sqlbind.RebindArgs(query)
	stmt, err := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, rewritten)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return stmt, nil
	}
	return &rebindStmt{Stmt: stmt, order: order}, nil
}

func (c *rebindConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *rebindConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	rewritten, order := sqlbind.RebindArgs(query)
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, rewritten, reorder(args, order))
}

func (c *rebindConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rewritten, order := sqlbind.RebindArgs(query)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, rewritten, reorder(args, order))
}

func (c *rebindConn) Ping(ctx context.Context) error {
	return c.Conn.(driver.Pinger).Ping(ctx)
}

func (c *rebindConn) ResetSession(ctx context.Context) error {
	return c.Conn.(driver.SessionResetter).ResetSession(ctx)
}

func (c *rebindConn) IsValid() bool { return c.Conn.(driver.Validator).IsValid() }

func (c *rebindConn) CheckNamedValue(nv *driver.NamedValue) error {
	return c.Conn.(driver.NamedValueChecker).CheckNamedValue(nv)
}

// rebindStmt is a prepared statement whose placeholders were reordered: it
// takes the caller's arguments and supplies them in the order the "?" appear.
type rebindStmt struct {
	driver.Stmt
	order []int
}

// NumInput reports -1 (unknown): the caller's argument count differs from the
// number of "?" when a placeholder is used twice.
func (s *rebindStmt) NumInput() int { return -1 }

func (s *rebindStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.Stmt.(driver.StmtExecContext).ExecContext(ctx, reorder(args, s.order))
}

func (s *rebindStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, reorder(args, s.order))
}

// reorder returns the arguments in the order the rewritten query's "?" take
// them; a nil order means the query had no numbered placeholders.
func reorder(args []driver.NamedValue, order []int) []driver.NamedValue {
	if order == nil {
		return args
	}
	out := make([]driver.NamedValue, len(order))
	for i, n := range order {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: args[n-1].Value}
	}
	return out
}
