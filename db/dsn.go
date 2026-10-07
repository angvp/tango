package db

import (
	"errors"
	"strings"
)

const sqliteScheme = "sqlite://"

// errUnsupportedDSN never echoes the rejected DSN, which may carry
// credentials.
var errUnsupportedDSN = errors.New("db: unsupported DSN; want sqlite://<path>, sqlite:///<absolute path>, sqlite://:memory:, or postgres://… (postgresql://… also works)")

// ParseDSN turns a scheme-qualified DSN into the Dialect, the database/sql
// driver name, and the driver's own DSN string to pass to sql.Open:
//
//	sqlite://app.db            SQLite file relative to the working directory
//	sqlite:///var/data/app.db  SQLite file at an absolute path
//	sqlite://:memory:          SQLite, in-memory
//	postgres://… postgresql://… PostgreSQL, passed to pgx unchanged
//
// Anything else, including a bare path such as "app.db", is an error. For
// SQLite the returned DSN already enables foreign key enforcement (see
// SQLiteForeignKeysDSN). ParseDSN never imports or registers a driver: the
// app still imports modernc.org/sqlite ("sqlite") or
// github.com/jackc/pgx/v5/stdlib ("pgx") itself.
func ParseDSN(dsn string) (dialect Dialect, driverName, driverDSN string, err error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return Postgres, "pgx", dsn, nil
	}
	path, ok := strings.CutPrefix(dsn, sqliteScheme)
	if !ok || path == "" || strings.HasPrefix(path, "?") {
		return SQLite, "", "", errUnsupportedDSN
	}
	return SQLite, "sqlite", SQLiteForeignKeysDSN(path), nil
}
