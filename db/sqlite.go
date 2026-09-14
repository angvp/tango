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
// tanGO's generated project scaffold uses this when building its SQLite
// DSN; anyone opening their own *sql.DB with driver "sqlite" (as opposed to
// going through the scaffold) should call it too if they rely on foreign
// key constraints — see docs/guides/reusable-apps.md and the relationships
// guide for why this isn't automatic inside db.NewStore itself (NewStore
// wraps an already-open *sql.DB and must never touch it, so the flags that
// don't need a database — "-check", "-tango-dump-models" — stay
// database-free).
func SQLiteForeignKeysDSN(dsn string) string {
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_foreign_keys=on"
}
