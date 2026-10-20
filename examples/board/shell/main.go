// Command shell is the board's "tango shell": an interactive Go console with
// the board's apps, models and database loaded and nothing served. Run it
// with "tango shell". It executes local code with the board's database
// credentials: it is not a sandbox.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/shell"

	"board/project"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func main() {
	os.Exit(run())
}

func run() int {
	if err := tango.LoadEnvFile(".env"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, dsn.Dialect)
	return shell.Run(context.Background(), project.Config(store), store, os.Args[1:], shell.Options{
		DatabaseLabel: databaseLabel(dsn),
	})
}

// databaseLabel says which database the shell works on, without any part
// of the DSN that could be a credential: for SQLite the file, for PostgreSQL
// only the host and database name. Anything it cannot read with confidence
// is hidden rather than shown.
func databaseLabel(dsn db.DSN) string {
	switch dsn.Dialect {
	case db.SQLite:
		path, _, _ := strings.Cut(dsn.Source, "?")
		return "sqlite: " + strings.TrimPrefix(path, "file:")
	case db.MySQL:
		return mysqlDatabaseLabel(dsn.Source)
	case db.Postgres:
	default:
		return "database: (address hidden)"
	}
	const hidden = "postgres: (address hidden)"
	_, rest, ok := strings.Cut(dsn.Source, "://")
	if !ok {
		return hidden
	}
	// The address ends at the first "/", "?" or "#". A "@" after that means
	// the userinfo was not escaped, so what follows cannot be told apart
	// from a password.
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}
	if strings.Count(rest, "@") != strings.Count(authority, "@") || strings.Count(authority, "@") > 1 {
		return hidden
	}
	u, err := url.Parse(dsn.Source)
	if err != nil || u.Host == "" {
		return hidden
	}
	return "postgres: " + u.Host + "/" + strings.TrimPrefix(u.Path, "/")
}

// mysqlDatabaseLabel labels a MySQL DSN, which the driver writes as
// user:password@tcp(host:port)/database?options, with only host:port and the
// database. The address starts after the last "@tcp(", since the password
// may hold anything.
func mysqlDatabaseLabel(source string) string {
	const hidden = "mysql: (address hidden)"
	start := strings.LastIndex(source, "@tcp(")
	if start < 0 {
		return hidden
	}
	address, rest, ok := strings.Cut(source[start+len("@tcp("):], ")/")
	if !ok || address == "" {
		return hidden
	}
	database, _, _ := strings.Cut(rest, "?")
	return "mysql: " + address + "/" + database
}
