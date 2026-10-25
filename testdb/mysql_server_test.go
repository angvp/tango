package testdb

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/angvp/tango/db"
	"github.com/go-sql-driver/mysql"
)

// mysqlAdmin opens an administrative connection to the run's server, outside
// any per-test database, and closes it when the test ends.
func mysqlAdmin(t testing.TB) *sql.DB {
	t.Helper()
	config, err := mysql.ParseDSN(parseRunDSN(t).Source)
	if err != nil {
		t.Fatalf("testdb: parse mysql dsn: %v", err)
	}
	config.DBName = ""
	admin, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("testdb: open mysql admin: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	return admin
}

func mysqlSchemaExists(t testing.TB, admin *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := admin.QueryRow(`SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?`, name).Scan(&count); err != nil {
		t.Fatalf("look up database %s: %v", name, err)
	}
	return count == 1
}

func TestOpenMySQLGivesAUsableDatabaseDroppedAfterCleanup(t *testing.T) {
	MySQLOnly(t, "checks the per-test database, which only a MySQL run creates")
	admin := mysqlAdmin(t)

	var name, charset, collation string
	rec := runRecording(t, func(tb *recordingTB) {
		sqlDB := openMySQL(tb, parseRunDSN(tb).Source)
		if err := sqlDB.QueryRow(`SELECT DATABASE()`).Scan(&name); err != nil {
			tb.Fatalf("database name: %v", err)
		}
		if err := sqlDB.QueryRow(`SELECT DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = DATABASE()`).Scan(&charset, &collation); err != nil {
			tb.Fatalf("database collation: %v", err)
		}
		if _, err := sqlDB.Exec(`CREATE TABLE tangocov_probe (id BIGINT PRIMARY KEY)`); err != nil {
			tb.Fatalf("create table: %v", err)
		}
		var sum int
		if err := sqlDB.QueryRow(`SELECT CAST($1 AS SIGNED) + CAST($1 AS SIGNED)`, 21).Scan(&sum); err != nil {
			tb.Fatalf("numbered placeholder: %v", err)
		}
		if sum != 42 {
			tb.Fatalf("repeated placeholder sum = %d, want 42", sum)
		}
	})
	if rec.failed {
		t.Fatalf("openMySQL failed: %v", rec.messages)
	}
	if !strings.HasPrefix(name, "tango_test_") {
		t.Fatalf("database = %q, want a tango_test_ database", name)
	}
	if charset != "utf8mb4" || collation != "utf8mb4_bin" {
		t.Fatalf("database charset/collation = %s/%s, want utf8mb4/utf8mb4_bin", charset, collation)
	}
	if !mysqlSchemaExists(t, admin, name) {
		t.Fatalf("database %s does not exist while its test runs", name)
	}

	rec.runCleanups()
	if len(rec.errors) != 0 {
		t.Fatalf("cleanup reported errors: %v", rec.errors)
	}
	if mysqlSchemaExists(t, admin, name) {
		t.Errorf("database %s still exists after cleanup", name)
	}
}

func TestOpenMySQLGivesEachOpenItsOwnDatabase(t *testing.T) {
	MySQLOnly(t, "checks per-open databases, which only a MySQL run creates")
	admin := mysqlAdmin(t)

	var first, second string
	rec := runRecording(t, func(tb *recordingTB) {
		source := parseRunDSN(tb).Source
		a := openMySQL(tb, source)
		b := openMySQL(tb, source)
		if err := a.QueryRow(`SELECT DATABASE()`).Scan(&first); err != nil {
			tb.Fatalf("database name: %v", err)
		}
		if err := b.QueryRow(`SELECT DATABASE()`).Scan(&second); err != nil {
			tb.Fatalf("database name: %v", err)
		}
		if _, err := a.Exec(`CREATE TABLE tangocov_probe (id BIGINT PRIMARY KEY)`); err != nil {
			tb.Fatalf("create table in first database: %v", err)
		}
		var visible int
		if err := b.QueryRow(`SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'tangocov_probe'`).Scan(&visible); err != nil {
			tb.Fatalf("look up probe in second database: %v", err)
		}
		if visible != 0 {
			tb.Fatalf("second database sees the first one's table")
		}
	})
	if rec.failed {
		t.Fatalf("openMySQL failed: %v", rec.messages)
	}
	if first == second {
		t.Fatalf("two opens share database %q", first)
	}

	rec.runCleanups()
	if len(rec.errors) != 0 {
		t.Fatalf("cleanup reported errors: %v", rec.errors)
	}
	for _, name := range []string{first, second} {
		if mysqlSchemaExists(t, admin, name) {
			t.Errorf("database %s still exists after cleanup", name)
		}
	}
}

func TestOpenMySQLReportsCreateDatabaseFailureWithoutTheCredentials(t *testing.T) {
	MySQLOnly(t, "needs a server on which a low-privilege account can be created")
	admin := mysqlAdmin(t)

	user := "tangocov_" + randomHex(t, 6)
	password := randomHex(t, 16)
	if _, err := admin.Exec("CREATE USER '" + user + "'@'%' IDENTIFIED BY '" + password + "'"); err != nil {
		t.Fatalf("create low-privilege user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), "DROP USER IF EXISTS '"+user+"'@'%'")
	})

	config, err := mysql.ParseDSN(parseRunDSN(t).Source)
	if err != nil {
		t.Fatalf("parse mysql dsn: %v", err)
	}
	config.User = user
	config.Passwd = password

	rec := runRecording(t, func(tb *recordingTB) {
		openMySQL(tb, config.FormatDSN())
	})
	if !rec.failed {
		t.Fatalf("openMySQL succeeded for an account without CREATE")
	}
	if len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "create database") {
		t.Fatalf("failure messages = %v, want one containing %q", rec.messages, "create database")
	}
	for _, msg := range rec.messages {
		if strings.Contains(msg, password) {
			t.Fatalf("failure message leaks the password: %q", msg)
		}
	}
}

func TestOpenMySQLReportsADropThatFailsAtCleanup(t *testing.T) {
	MySQLOnly(t, "needs a server to create and drop the per-test database")
	admin := mysqlAdmin(t)

	var name string
	rec := runRecording(t, func(tb *recordingTB) {
		sqlDB := openMySQL(tb, parseRunDSN(tb).Source)
		if err := sqlDB.QueryRow(`SELECT DATABASE()`).Scan(&name); err != nil {
			tb.Fatalf("database name: %v", err)
		}
	})
	if rec.failed {
		t.Fatalf("openMySQL failed: %v", rec.messages)
	}
	if !mysqlSchemaExists(t, admin, name) {
		t.Fatalf("database %s missing before the test removes it", name)
	}
	if _, err := admin.Exec("DROP DATABASE `" + name + "`"); err != nil {
		t.Fatalf("drop database out from under cleanup: %v", err)
	}

	rec.runCleanups()
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "drop database") {
		t.Fatalf("cleanup errors = %v, want one containing %q", rec.errors, "drop database")
	}
}

func TestOpenMySQLRejectsAMalformedDSN(t *testing.T) {
	rec := runRecording(t, func(tb *recordingTB) {
		openMySQL(tb, "not-a-mysql-dsn")
	})
	if !rec.failed || len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "not a MySQL DSN") {
		t.Fatalf("malformed DSN: failed=%v messages=%v, want a not-a-MySQL-DSN failure", rec.failed, rec.messages)
	}
}

func TestOpenMySQLFailsWhenTheServerIsUnreachable(t *testing.T) {
	rec := runRecording(t, func(tb *recordingTB) {
		openMySQL(tb, "tango:tango@tcp(127.0.0.1:1)/mysql?timeout=2s")
	})
	if !rec.failed || len(rec.messages) != 1 || !strings.Contains(rec.messages[0], "cannot be reached") {
		t.Fatalf("unreachable server: failed=%v messages=%v, want a cannot-be-reached failure", rec.failed, rec.messages)
	}
}

func TestOpenDialectMySQLReturnsAUsableHandle(t *testing.T) {
	MySQLOnly(t, "checks Open's MySQL branch, which only a MySQL run takes")
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
	if gotDialect != db.MySQL {
		t.Fatalf("dialect = %v, want db.MySQL", gotDialect)
	}
}
