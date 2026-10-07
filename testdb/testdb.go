// Package testdb gives each test a fresh database for the run's Test
// dialect, so one test suite runs unchanged against SQLite or PostgreSQL.
//
// The run's dialect comes from one variable, TANGO_TEST_DSN, read by its
// scheme through db.ParseDSN, the same grammar an app's TANGO_DB_DSN uses:
//
//	unset or sqlite://:memory:   SQLite, in memory
//	sqlite://<any path>          SQLite, a new file per test under t.TempDir()
//	postgres://… postgresql://…  PostgreSQL, a new schema per test
//
// One scheme-qualified variable, rather than a dialect variable beside a
// DSN, means the two can never disagree, and anyone who can configure an
// app already knows how to point its tests somewhere. The SQLite file path
// only selects file-backed mode; it is never opened, because a file shared
// between tests would leak rows from one into the next.
//
// A PostgreSQL run gets a schema created for each Open, set as every
// connection's search_path and dropped with CASCADE when the test ends, so
// tests may commit, run DDL and run in parallel without seeing each other.
// An unreachable server or a malformed DSN fails the test: a PostgreSQL run
// never passes by skipping. Tests that cannot run on one dialect say so with
// SQLiteOnly or PostgresOnly and a reason, so every exemption is greppable.
//
// testdb imports the modernc.org/sqlite and pgx drivers itself, so tests
// need no driver imports of their own.
//
// Raw SQL in a test can use PostgreSQL's numbered placeholders ($1, $2, …)
// and TRUE/FALSE on either dialect: modernc.org/sqlite binds $N by number.
package testdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/angvp/tango/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// envVar names the variable that selects the run's Test dialect.
const envVar = "TANGO_TEST_DSN"

const sqliteMemoryDSN = "sqlite://:memory:"

// connectTimeout bounds reaching Postgres, so an unreachable server fails
// the test promptly instead of hanging it.
const connectTimeout = 10 * time.Second

// Open returns a fresh database for this test and the Dialect to use with it.
func Open(t testing.TB) (*sql.DB, db.Dialect) {
	t.Helper()
	if parsed := parseRunDSN(t); parsed.Dialect == db.Postgres {
		return openPostgres(t, parsed.Source), db.Postgres
	}
	return openSQLite(t), db.SQLite
}

// Store returns a db.Store over a fresh database for this test.
func Store(t testing.TB) *db.Store {
	t.Helper()
	return db.NewStore(Open(t))
}

// openSQLite returns an in-memory database, or, when the run's DSN names
// any SQLite file, a new file under the test's temporary directory. The
// DSN's own path is never used: a file shared between tests would leak
// rows from one into the next.
func openSQLite(t testing.TB) *sql.DB {
	t.Helper()
	inMemory := runDSN() == sqliteMemoryDSN
	dsn := sqliteMemoryDSN
	if !inMemory {
		dsn = "sqlite://" + filepath.Join(t.TempDir(), "test.db")
	}
	parsed, err := db.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	sqlDB, err := sql.Open(parsed.Driver, parsed.Source)
	if err != nil {
		t.Fatalf("testdb: open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if inMemory {
		// Each connection to :memory: is its own empty database, so the
		// pool must never open a second one.
		sqlDB.SetMaxOpenConns(1)
	}
	return sqlDB
}

// openPostgres creates a schema only this test uses and returns a database
// whose every connection has it as its search_path. The schema is dropped
// when the test ends.
func openPostgres(t testing.TB, dsn string) *sql.DB {
	t.Helper()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("testdb: %s: %v", envVar, err)
	}

	admin := stdlib.OpenDB(*config.Copy())
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("testdb: %s names a Postgres database that cannot be reached: %v", envVar, err)
	}

	schema := pgx.Identifier{newSchemaName(t)}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("testdb: create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("testdb: drop schema %s: %v", schema, err)
		}
	})

	scoped := config.Copy()
	scoped.RuntimeParams["search_path"] = schema
	sqlDB := stdlib.OpenDB(*scoped)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlDB
}

// newSchemaName returns a schema name no other test, run, or concurrent
// process will pick.
func newSchemaName(t testing.TB) string {
	t.Helper()
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("testdb: schema name: %v", err)
	}
	return "tango_test_" + hex.EncodeToString(suffix)
}

// Dialect reports the run's Test dialect. It has no test to fail, so an
// unsupported TANGO_TEST_DSN panics with the message Open, Store,
// SQLiteOnly and PostgresOnly fail the test with.
func Dialect() db.Dialect {
	parsed, err := db.ParseDSN(runDSN())
	if err != nil {
		panic(runDSNError(err))
	}
	return parsed.Dialect
}

// SQLiteOnly skips the test unless the run's Test dialect is SQLite. The
// reason says what the test needs that PostgreSQL cannot give it.
func SQLiteOnly(t testing.TB, reason string) {
	t.Helper()
	if parseRunDSN(t).Dialect != db.SQLite {
		t.Skipf("testdb: SQLite only: %s", reason)
	}
}

// PostgresOnly skips the test unless the run's Test dialect is PostgreSQL.
// The reason says what the test needs that SQLite cannot give it.
func PostgresOnly(t testing.TB, reason string) {
	t.Helper()
	if parseRunDSN(t).Dialect != db.Postgres {
		t.Skipf("testdb: PostgreSQL only: %s", reason)
	}
}

// parseRunDSN parses the run's TANGO_TEST_DSN, failing the test when its
// scheme is unsupported.
func parseRunDSN(t testing.TB) db.DSN {
	t.Helper()
	parsed, err := db.ParseDSN(runDSN())
	if err != nil {
		t.Fatalf("%s", runDSNError(err))
	}
	return parsed
}

// runDSNError names TANGO_TEST_DSN in a parse failure. It never echoes
// the DSN itself, which may carry credentials.
func runDSNError(err error) string {
	return fmt.Sprintf("testdb: %s: %v", envVar, err)
}

// runDSN is the run's TANGO_TEST_DSN, with unset meaning in-memory SQLite.
func runDSN() string {
	if dsn := os.Getenv(envVar); dsn != "" {
		return dsn
	}
	return sqliteMemoryDSN
}
