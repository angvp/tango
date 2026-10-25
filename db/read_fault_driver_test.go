package db_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/angvp/tango/db"
)

// errFault is the row error a faultConnector's rows report.
var errFault = errors.New("fault: injected row error")

type faultMode int

const (
	// faultNextError fails the first Next with errFault, before any row.
	faultNextError faultMode = iota
	// faultBadScan returns one row whose id is bytes that cannot scan into an
	// int64.
	faultBadScan
)

// faultConnector is a database/sql connector whose queries return rows
// shaped by mode. Only the read paths of First and Exists reach it.
type faultConnector struct{ mode faultMode }

func (c faultConnector) Connect(context.Context) (driver.Conn, error) {
	return &faultConn{mode: c.mode}, nil
}

func (c faultConnector) Driver() driver.Driver { return faultDriver{} }

type faultDriver struct{}

func (faultDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fault: Open is unused; the connector is the entry point")
}

type faultConn struct{ mode faultMode }

func (c *faultConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fault: prepared statements are unsupported")
}
func (c *faultConn) Close() error { return nil }
func (c *faultConn) Begin() (driver.Tx, error) {
	return nil, errors.New("fault: transactions are unsupported")
}

func (c *faultConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &faultRows{mode: c.mode}, nil
}

type faultRows struct {
	mode faultMode
	done bool
}

// Columns matches createWidget's fields in declaration order.
func (r *faultRows) Columns() []string {
	return []string{"id", "name", "active", "count", "score", "created_at"}
}

func (r *faultRows) Close() error { return nil }

func (r *faultRows) Next(dest []driver.Value) error {
	if r.mode == faultNextError {
		return errFault
	}
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = []byte("not-an-int")
	for i := 1; i < len(dest); i++ {
		dest[i] = nil
	}
	return nil
}

func TestFirstReportsARowErrorAsItsOwnError(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := db.NewStore(sql.OpenDB(faultConnector{mode: faultNextError}), db.SQLite)

	var dest createWidget
	err := store.First(context.Background(), meta, db.Query{}, &dest)
	if !errors.Is(err, errFault) {
		t.Fatalf("First error = %v, want the row error", err)
	}
	if errors.Is(err, db.ErrNotFound) {
		t.Errorf("a row error must not read as not-found: %v", err)
	}
	if !reflect.DeepEqual(dest, createWidget{}) {
		t.Errorf("dest changed on a row error: %+v", dest)
	}
}

func TestExistsReportsARowErrorInsteadOfNoMatch(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := db.NewStore(sql.OpenDB(faultConnector{mode: faultNextError}), db.SQLite)

	found, err := store.Exists(context.Background(), meta, db.Query{})
	if !errors.Is(err, errFault) {
		t.Fatalf("Exists error = %v, want the row error", err)
	}
	if found {
		t.Errorf("Exists reported a match on a row error")
	}
}

func TestFirstRefusesARowThatDoesNotScanAndLeavesDestAlone(t *testing.T) {
	meta := registerModel(t, createWidget{})
	store := db.NewStore(sql.OpenDB(faultConnector{mode: faultBadScan}), db.SQLite)
	sentinel := createWidget{ID: 99, Name: "keep"}

	dest := sentinel
	err := store.First(context.Background(), meta, db.Query{}, &dest)
	if err == nil {
		t.Fatal("First accepted a row whose id does not scan into an int64")
	}
	if errors.Is(err, db.ErrNotFound) {
		t.Errorf("a scan failure must not read as not-found: %v", err)
	}
	if !reflect.DeepEqual(dest, sentinel) {
		t.Errorf("dest changed on a scan failure: %+v", dest)
	}
}
