package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	_ "modernc.org/sqlite"
)

// TestParseDSNMakesASecondSQLiteWriterWaitForTheLock checks that a SQLite
// file opened through ParseDSN makes a writer that finds the write lock
// held wait for it, rather than fail at once with SQLITE_BUSY: an app on a
// SQLite file serves concurrent writes. It opens its own file, not a
// testdb database, because what it checks is ParseDSN's SQLite source
// whatever the run's Test dialect.
func TestParseDSNMakesASecondSQLiteWriterWaitForTheLock(t *testing.T) {
	parsed, err := db.ParseDSN("sqlite://" + filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("ParseDSN: %v", err)
	}
	open := func() *sql.DB {
		sqlDB, err := sql.Open(parsed.Driver, parsed.Source)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		return sqlDB
	}
	holder, waiter := open(), open()

	if _, err := holder.Exec(`CREATE TABLE note (n INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	tx, err := holder.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`INSERT INTO note (n) VALUES (1)`); err != nil {
		t.Fatalf("first write: %v", err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		released <- tx.Commit()
	}()

	if _, err := waiter.Exec(`INSERT INTO note (n) VALUES (2)`); err != nil {
		t.Fatalf("second write = %v, want it to wait for the first writer's lock", err)
	}
	if err := <-released; err != nil {
		t.Fatalf("commit: %v", err)
	}
}
