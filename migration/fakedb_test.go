package migration

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeDB is a database/sql driver that records every statement and answers
// queries from canned rows. It lets the PostgreSQL and MySQL branches of the
// executors run, and their SQL be checked, without a server.
type fakeDB struct {
	mu      sync.Mutex
	log     []string
	answers []fakeAnswer
}

// fakeAnswer answers a query whose text contains match.
type fakeAnswer struct {
	match string
	rows  [][]driver.Value
	err   error
}

func newFakeDB(t *testing.T, answers ...fakeAnswer) (*sql.DB, *fakeDB) {
	t.Helper()
	f := &fakeDB{answers: answers}
	sqlDB := sql.OpenDB(fakeConnector{f})
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB, f
}

// statements returns what was executed or queried, in order, with the
// transaction markers.
func (f *fakeDB) statements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

func (f *fakeDB) record(entry string) {
	f.mu.Lock()
	f.log = append(f.log, entry)
	f.mu.Unlock()
}

// executed returns only the statements that changed the schema or data, not
// the reads the executor made first.
func (f *fakeDB) executed() []string {
	var out []string
	for _, s := range f.statements() {
		if !strings.HasPrefix(s, "QUERY ") {
			out = append(out, s)
		}
	}
	return out
}

type fakeConnector struct{ f *fakeDB }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) { return &fakeConn{c.f}, nil }
func (c fakeConnector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, fmt.Errorf("use the connector") }

type fakeConn struct{ f *fakeDB }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) { return &fakeStmt{c.f, query}, nil }
func (c *fakeConn) Close() error                              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) {
	c.f.record("BEGIN")
	return &fakeTx{c.f}, nil
}

type fakeTx struct{ f *fakeDB }

func (t *fakeTx) Commit() error   { t.f.record("COMMIT"); return nil }
func (t *fakeTx) Rollback() error { t.f.record("ROLLBACK"); return nil }

type fakeStmt struct {
	f     *fakeDB
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }

func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	s.f.record(s.query)
	for _, a := range s.f.answers {
		if a.err != nil && a.rows == nil && strings.Contains(s.query, a.match) {
			return nil, a.err
		}
	}
	return driver.RowsAffected(0), nil
}

func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	s.f.record("QUERY " + s.query)
	for _, a := range s.f.answers {
		if strings.Contains(s.query, a.match) {
			if a.err != nil {
				return nil, a.err
			}
			return &fakeRows{rows: a.rows}, nil
		}
	}
	return &fakeRows{}, nil
}

type fakeRows struct {
	rows [][]driver.Value
	next int
}

func (r *fakeRows) Columns() []string {
	n := 0
	if len(r.rows) > 0 {
		n = len(r.rows[0])
	}
	cols := make([]string, n)
	for i := range cols {
		cols[i] = fmt.Sprintf("c%d", i)
	}
	return cols
}
func (r *fakeRows) Close() error { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.next >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.next])
	r.next++
	return nil
}

// mysqlColumnRow is what information_schema.columns returns for a column:
// data_type, is_nullable, column_default, column_type, extra.
func mysqlColumnRow(dataType, nullable string, columnDefault any, columnType, extra string) fakeAnswer {
	return fakeAnswer{match: "information_schema.columns", rows: [][]driver.Value{{dataType, nullable, columnDefault, columnType, extra}}}
}

func nullCountRow(n int64) fakeAnswer {
	return fakeAnswer{match: "COUNT(*)", rows: [][]driver.Value{{n}}}
}

// rowsOf is a single-column result with one row.
func rowsOf(v driver.Value) [][]driver.Value { return [][]driver.Value{{v}} }
