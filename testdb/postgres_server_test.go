package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// pgAdmin opens an administrative connection to the run's server, outside
// any per-test schema, and closes it when the test ends.
func pgAdmin(t testing.TB) *sql.DB {
	t.Helper()
	config, err := pgx.ParseConfig(parseRunDSN(t).Source)
	if err != nil {
		t.Fatalf("testdb: parse postgres dsn: %v", err)
	}
	admin := stdlib.OpenDB(*config)
	t.Cleanup(func() { _ = admin.Close() })
	return admin
}

func pgSchemaExists(t testing.TB, admin *sql.DB, schema string) bool {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)`, schema).Scan(&exists); err != nil {
		t.Fatalf("look up schema %s: %v", schema, err)
	}
	return exists
}

func pgCurrentSchema(t testing.TB, sqlDB *sql.DB) string {
	t.Helper()
	var schema string
	if err := sqlDB.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	return schema
}

func TestOpenPostgresGivesAUsableSchemaDroppedAfterCleanup(t *testing.T) {
	PostgresOnly(t, "checks the per-test schema, which only a Postgres run creates")
	admin := pgAdmin(t)

	var schema string
	rec := runRecording(t, func(tb *recordingTB) {
		sqlDB := openPostgres(tb, parseRunDSN(tb).Source)
		schema = pgCurrentSchema(tb, sqlDB)
		if _, err := sqlDB.Exec(`CREATE TABLE tangocov_probe (id BIGINT PRIMARY KEY)`); err != nil {
			tb.Fatalf("create table: %v", err)
		}
		var sum int
		if err := sqlDB.QueryRow(`SELECT $1::int + $1::int`, 21).Scan(&sum); err != nil {
			tb.Fatalf("numbered placeholder: %v", err)
		}
		if sum != 42 {
			tb.Fatalf("repeated placeholder sum = %d, want 42", sum)
		}
	})
	if rec.failed {
		t.Fatalf("openPostgres failed: %v", rec.messages)
	}
	if !strings.HasPrefix(schema, "tango_test_") {
		t.Fatalf("schema = %q, want a tango_test_ schema", schema)
	}
	if !pgSchemaExists(t, admin, schema) {
		t.Fatalf("schema %s does not exist while its test runs", schema)
	}

	rec.runCleanups()
	if len(rec.errors) != 0 {
		t.Fatalf("cleanup reported errors: %v", rec.errors)
	}
	if pgSchemaExists(t, admin, schema) {
		t.Errorf("schema %s still exists after cleanup", schema)
	}
}

func TestOpenPostgresGivesEachOpenItsOwnSchema(t *testing.T) {
	PostgresOnly(t, "checks per-open schemas, which only a Postgres run creates")
	admin := pgAdmin(t)

	var first, second string
	rec := runRecording(t, func(tb *recordingTB) {
		dsn := parseRunDSN(tb).Source
		a := openPostgres(tb, dsn)
		b := openPostgres(tb, dsn)
		first, second = pgCurrentSchema(tb, a), pgCurrentSchema(tb, b)
		if _, err := a.Exec(`CREATE TABLE tangocov_probe (id BIGINT PRIMARY KEY)`); err != nil {
			tb.Fatalf("create table in first schema: %v", err)
		}
	})
	if rec.failed {
		t.Fatalf("openPostgres failed: %v", rec.messages)
	}
	if first == second {
		t.Fatalf("two opens share schema %q", first)
	}
	if !pgSchemaExists(t, admin, first) || !pgSchemaExists(t, admin, second) {
		t.Fatalf("both schemas must exist while their opens are live")
	}

	rec.runCleanups()
	if len(rec.errors) != 0 {
		t.Fatalf("cleanup reported errors: %v", rec.errors)
	}
	for _, schema := range []string{first, second} {
		if pgSchemaExists(t, admin, schema) {
			t.Errorf("schema %s still exists after cleanup", schema)
		}
	}
}

func TestOpenPostgresReportsCreateSchemaFailureWithoutTheCredentials(t *testing.T) {
	PostgresOnly(t, "needs a server on which a low-privilege role can be created")
	admin := pgAdmin(t)

	role := "tangocov_" + randomHex(t, 6)
	password := randomHex(t, 16)
	roleIdent := pgx.Identifier{role}.Sanitize()
	if _, err := admin.Exec(`CREATE ROLE ` + roleIdent + ` LOGIN PASSWORD '` + password + `'`); err != nil {
		t.Fatalf("create low-privilege role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP ROLE IF EXISTS `+roleIdent)
	})

	u, err := url.Parse(parseRunDSN(t).Source)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	u.User = url.UserPassword(role, password)

	rec := runRecording(t, func(tb *recordingTB) {
		openPostgres(tb, u.String())
	})
	if !rec.failed {
		t.Fatalf("openPostgres succeeded for a role without CREATE on the database")
	}
	if len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "create schema") {
		t.Fatalf("failure messages = %v, want one containing %q", rec.messages, "create schema")
	}
	for _, msg := range rec.messages {
		if strings.Contains(msg, password) {
			t.Fatalf("failure message leaks the password: %q", msg)
		}
	}
}

func TestOpenPostgresReportsADropThatFailsAtCleanup(t *testing.T) {
	PostgresOnly(t, "needs a server to create and drop the per-test schema")
	admin := pgAdmin(t)

	var schema string
	rec := runRecording(t, func(tb *recordingTB) {
		sqlDB := openPostgres(tb, parseRunDSN(tb).Source)
		schema = pgCurrentSchema(tb, sqlDB)
	})
	if rec.failed {
		t.Fatalf("openPostgres failed: %v", rec.messages)
	}
	if !pgSchemaExists(t, admin, schema) {
		t.Fatalf("schema %s missing before the test removes it", schema)
	}
	if _, err := admin.Exec(`DROP SCHEMA ` + pgx.Identifier{schema}.Sanitize() + ` CASCADE`); err != nil {
		t.Fatalf("drop schema out from under cleanup: %v", err)
	}

	rec.runCleanups()
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "drop schema") {
		t.Fatalf("cleanup errors = %v, want one containing %q", rec.errors, "drop schema")
	}
}

func TestOpenPostgresRejectsAMalformedDSN(t *testing.T) {
	rec := runRecording(t, func(tb *recordingTB) {
		openPostgres(tb, "postgres://%zz")
	})
	if !rec.failed || len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "TANGO_TEST_DSN") {
		t.Fatalf("malformed DSN: failed=%v messages=%v, want a TANGO_TEST_DSN failure", rec.failed, rec.messages)
	}
}

func TestOpenPostgresFailsWhenTheServerIsUnreachable(t *testing.T) {
	dsn := "postgres://tango:tango@127.0.0.1:1/tango?sslmode=disable&connect_timeout=2"
	rec := runRecording(t, func(tb *recordingTB) {
		openPostgres(tb, dsn)
	})
	if !rec.failed || len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "cannot be reached") {
		t.Fatalf("unreachable server: failed=%v messages=%v, want a cannot-be-reached failure", rec.failed, rec.messages)
	}
}

func TestOpenDialectPostgresReturnsAUsableHandle(t *testing.T) {
	PostgresOnly(t, "checks Open's Postgres branch, which only a Postgres run takes")
	var gotDialect db.Dialect
	rec := runRecording(t, func(tb *recordingTB) {
		sqlDB, dialect := Open(tb)
		if err := sqlDB.Ping(); err != nil {
			tb.Fatalf("ping: %v", err)
		}
		gotDialect = dialect
	})
	if rec.failed {
		t.Fatalf("Open failed: %v", rec.messages)
	}
	if gotDialect != db.Postgres {
		t.Fatalf("dialect = %v, want db.Postgres", gotDialect)
	}
}
