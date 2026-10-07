package db

import "strings"

// SQLiteForeignKeysDSN appends the modernc.org/sqlite driver's
// "_foreign_keys=on" query parameter to a SQLite DSN, so every connection
// the driver opens for it enforces FOREIGN KEY constraints (SQLite treats
// this as a per-connection setting, off by default). This is a plain string
// helper — it never imports the driver itself, keeping db driver-agnostic —
// so it composes with any DSN shape (a file path, ":memory:", or one that
// already carries other query parameters).
//
// ParseDSN applies it to every sqlite:// DSN, so an app that opens its
// database from ParseDSN's result (as tango.LoadDBConfigFromEnv, and so
// the generated scaffold, does) already enforces foreign keys. Anyone
// opening their own *sql.DB with driver "sqlite" from a bare path should
// call it too if they rely on foreign key constraints. It isn't automatic
// inside db.NewStore itself: NewStore wraps an already-open *sql.DB and
// must never touch it, so the flags that don't need a database ("-check",
// "-tango-dump-models") stay database-free.
func SQLiteForeignKeysDSN(dsn string) string {
	return withSQLiteParam(dsn, "_foreign_keys=on")
}

// sqliteBusyTimeoutParam makes a connection that finds another
// connection's write lock held wait up to 5 seconds for it (Django's SQLite
// default) instead of failing at once with SQLITE_BUSY.
const sqliteBusyTimeoutParam = "_pragma=busy_timeout(5000)"

// withSQLiteBusyTimeout adds sqliteBusyTimeoutParam to dsn unless dsn
// already sets a busy timeout of its own.
func withSQLiteBusyTimeout(dsn string) string {
	if strings.Contains(dsn, "busy_timeout") {
		return dsn
	}
	return withSQLiteParam(dsn, sqliteBusyTimeoutParam)
}

// withSQLiteParam appends one query parameter to a SQLite DSN.
func withSQLiteParam(dsn, param string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + param
}
