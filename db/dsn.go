package db

import (
	"errors"
	"strings"
)

const sqliteScheme = "sqlite://"

// errUnsupportedDSN never echoes the rejected DSN, which may carry
// credentials.
var errUnsupportedDSN = errors.New("db: unsupported DSN; want sqlite://<path>, sqlite:///<absolute path>, sqlite://:memory:, or postgres://… (postgresql://… also works)")

// DSN is a parsed scheme-qualified DSN: the Dialect to build a Store
// with, and the two arguments to pass to sql.Open.
type DSN struct {
	// Dialect is the SQL dialect the DSN's scheme names.
	Dialect Dialect
	// Driver is the database/sql driver name: "sqlite" or "pgx".
	Driver string
	// Source is the driver's own data source name.
	Source string
}

// ParseDSN turns a scheme-qualified DSN into the Dialect, the database/sql
// driver name, and the driver's own data source name to pass to sql.Open:
//
//	sqlite://app.db            SQLite file relative to the working directory
//	sqlite:///var/data/app.db  SQLite file at an absolute path
//	sqlite://:memory:          SQLite, in-memory
//	postgres://… postgresql://… PostgreSQL, passed to pgx unchanged
//
// Anything else, including a bare path such as "app.db", is an error. For
// SQLite the returned Source already enables foreign key enforcement (see
// SQLiteForeignKeysDSN) and makes a writer wait up to 5 seconds for another
// connection's write lock instead of failing with SQLITE_BUSY, unless the
// DSN sets its own busy_timeout. ParseDSN never imports or registers a driver: the
// app still imports modernc.org/sqlite ("sqlite") or
// github.com/jackc/pgx/v5/stdlib ("pgx") itself.
func ParseDSN(dsn string) (DSN, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return DSN{Dialect: Postgres, Driver: "pgx", Source: dsn}, nil
	}
	path, ok := strings.CutPrefix(dsn, sqliteScheme)
	if !ok || path == "" || strings.HasPrefix(path, "?") {
		return DSN{}, errUnsupportedDSN
	}
	return DSN{Dialect: SQLite, Driver: "sqlite", Source: SQLiteForeignKeysDSN(withSQLiteBusyTimeout(path))}, nil
}
