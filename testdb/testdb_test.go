package testdb_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/angvp/tango/testdb"
)

func TestOpenGivesEachCallAFreshDatabase(t *testing.T) {
	first, _ := testdb.Open(t)
	if _, err := first.Exec(`CREATE TABLE testdb_probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create table in first database: %v", err)
	}

	second, _ := testdb.Open(t)
	if _, err := second.Exec(`CREATE TABLE testdb_probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("second database already has the first one's table: %v", err)
	}
}

// fakeTB records how testdb ends a test instead of ending the real one.
// Fatal and Skip stop the calling goroutine just as testing.T does, so run
// must be used to call into testdb.
type fakeTB struct {
	testing.TB
	failed   bool
	skipped  bool
	messages []string
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (f *fakeTB) Skipf(format string, args ...any) {
	f.skipped = true
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

// run calls fn with a fakeTB on its own goroutine and waits for it,
// whether fn returns or the fake stops it.
func run(t *testing.T, fn func(tb testing.TB)) *fakeTB {
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

func TestOpenRejectsAnUnsupportedScheme(t *testing.T) {
	t.Setenv("TANGO_TEST_DSN", "mysql://root:secret@localhost/tango")

	fake := run(t, func(tb testing.TB) { testdb.Open(tb) })

	if !fake.failed {
		t.Fatal("Open with a mysql:// DSN did not fail the test")
	}
	message := strings.Join(fake.messages, "\n")
	for _, want := range []string{"TANGO_TEST_DSN", "sqlite://", "postgres://"} {
		if !strings.Contains(message, want) {
			t.Errorf("failure message %q does not mention %q", message, want)
		}
	}
	if strings.Contains(message, "secret") {
		t.Errorf("failure message %q echoes the DSN's password", message)
	}
}

func TestOpenFailsWhenPostgresIsUnreachable(t *testing.T) {
	// Port 1 on loopback refuses connections, so this fails fast.
	t.Setenv("TANGO_TEST_DSN", "postgres://tango:tango@127.0.0.1:1/tango_test?sslmode=disable&connect_timeout=5")

	fake := run(t, func(tb testing.TB) { testdb.Open(tb) })

	if fake.skipped {
		t.Fatalf("Open skipped an unreachable Postgres run: %v", fake.messages)
	}
	if !fake.failed {
		t.Fatal("Open with an unreachable Postgres DSN did not fail the test")
	}
}

func TestDialectFollowsTheDSNScheme(t *testing.T) {
	tests := []struct {
		dsn  string
		want db.Dialect
	}{
		{"", db.SQLite},
		{"sqlite://:memory:", db.SQLite},
		{"sqlite://ignored.db", db.SQLite},
		{"postgres://tango@localhost/tango_test", db.Postgres},
		{"postgresql://tango@localhost/tango_test", db.Postgres},
	}
	for _, test := range tests {
		t.Run(test.dsn, func(t *testing.T) {
			t.Setenv("TANGO_TEST_DSN", test.dsn)
			if got := testdb.Dialect(); got != test.want {
				t.Fatalf("Dialect() with %q = %v, want %v", test.dsn, got, test.want)
			}
		})
	}
}

func TestDialectPanicsOnAnUnsupportedScheme(t *testing.T) {
	t.Setenv("TANGO_TEST_DSN", "mysql://root:secret@localhost/tango")

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		got := testdb.Dialect()
		t.Errorf("Dialect() with a mysql:// DSN returned %v instead of panicking", got)
	}()

	message := fmt.Sprint(recovered)
	for _, want := range []string{"TANGO_TEST_DSN", "sqlite://", "postgres://"} {
		if !strings.Contains(message, want) {
			t.Errorf("panic message %q does not mention %q", message, want)
		}
	}
	if strings.Contains(message, "secret") {
		t.Errorf("panic message %q echoes the DSN's password", message)
	}
}

func TestExemptionsSkipOnlyTheOtherDialect(t *testing.T) {
	tests := []struct {
		name        string
		dsn         string
		exempt      func(testing.TB, string)
		wantSkipped bool
	}{
		{"SQLiteOnly on SQLite", "", testdb.SQLiteOnly, false},
		{"SQLiteOnly on Postgres", "postgres://tango@localhost/tango_test", testdb.SQLiteOnly, true},
		{"PostgresOnly on SQLite", "", testdb.PostgresOnly, true},
		{"PostgresOnly on Postgres", "postgres://tango@localhost/tango_test", testdb.PostgresOnly, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TANGO_TEST_DSN", test.dsn)

			fake := run(t, func(tb testing.TB) { test.exempt(tb, "needs a dialect-specific feature") })

			if fake.skipped != test.wantSkipped {
				t.Fatalf("skipped = %v, want %v", fake.skipped, test.wantSkipped)
			}
			if fake.skipped && !strings.Contains(strings.Join(fake.messages, "\n"), "needs a dialect-specific feature") {
				t.Fatalf("skip message %q does not carry the reason", fake.messages)
			}
		})
	}
}

func TestExemptionsFailOnAnUnsupportedScheme(t *testing.T) {
	t.Setenv("TANGO_TEST_DSN", "mysql://localhost/tango")
	for _, exempt := range []func(testing.TB, string){testdb.SQLiteOnly, testdb.PostgresOnly} {
		if fake := run(t, func(tb testing.TB) { exempt(tb, "reason") }); !fake.failed {
			t.Fatalf("exemption with an unsupported scheme did not fail: skipped=%v", fake.skipped)
		}
	}
}

func TestOpenGivesAFileBackedSQLiteRunAFreshFileAndIgnoresThePath(t *testing.T) {
	t.Setenv("TANGO_TEST_DSN", "sqlite://ignored.db")

	sqlDB, dialect := testdb.Open(t)

	if dialect != db.SQLite {
		t.Fatalf("dialect = %v, want SQLite", dialect)
	}
	var seq int
	var name, file string
	if err := sqlDB.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &file); err != nil {
		t.Fatalf("PRAGMA database_list: %v", err)
	}
	if file == "" || !filepath.IsAbs(file) {
		t.Fatalf("main database file = %q, want an absolute path to a file-backed database", file)
	}
	if filepath.Base(file) == "ignored.db" {
		t.Fatalf("main database file = %q, want the DSN's path ignored", file)
	}
	if _, err := os.Stat("ignored.db"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open created the DSN's own path ignored.db (stat err %v)", err)
	}

	var foreignKeys int
	if err := sqlDB.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, %v; want 1", foreignKeys, err)
	}
}

func TestStoreQueriesTheRunsDatabase(t *testing.T) {
	store := testdb.Store(t)

	var row struct{ Answer int }
	if err := store.QueryRow(context.Background(), &row, `SELECT 42 AS answer`); err != nil {
		t.Fatalf("QueryRow: %v", err)
	}
	if row.Answer != 42 {
		t.Fatalf("Answer = %d, want 42", row.Answer)
	}
}

func TestOpenGivesParallelPostgresTestsTheirOwnSchemaAndDropsIt(t *testing.T) {
	testdb.PostgresOnly(t, "checks per-test schemas, which only a Postgres run creates")

	schemas := make(chan string, 2)
	t.Run("group", func(t *testing.T) {
		for _, name := range []string{"first", "second"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				sqlDB, _ := testdb.Open(t)
				var schema string
				if err := sqlDB.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
					t.Fatalf("current_schema: %v", err)
				}
				if _, err := sqlDB.Exec(`CREATE TABLE testdb_probe (id BIGSERIAL PRIMARY KEY)`); err != nil {
					t.Fatalf("create table in %s: %v", schema, err)
				}
				schemas <- schema
			})
		}
	})
	close(schemas)

	first, second := <-schemas, <-schemas
	if first == "" || first == second || first == "public" || second == "public" {
		t.Fatalf("parallel tests used schemas %q and %q, want two distinct per-test schemas", first, second)
	}

	observer, _ := testdb.Open(t)
	for _, schema := range []string{first, second} {
		var exists bool
		err := observer.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&exists)
		if err != nil {
			t.Fatalf("look up schema %s: %v", schema, err)
		}
		if exists {
			t.Errorf("schema %s still exists after its test ended", schema)
		}
	}
}
