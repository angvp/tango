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
	if dsn.Dialect != db.Postgres {
		path, _, _ := strings.Cut(dsn.Source, "?")
		return "sqlite: " + strings.TrimPrefix(path, "file:")
	}
	const hidden = "postgres: (address hidden)"
	_, rest, ok := strings.Cut(dsn.Source, "://")
	if !ok {
		return hidden
	}
	// A "@" after the first "/" means the userinfo was not escaped, so what
	// follows cannot be told apart from a password.
	authority, _, _ := strings.Cut(rest, "/")
	if strings.Count(rest, "@") != strings.Count(authority, "@") || strings.Count(authority, "@") > 1 {
		return hidden
	}
	u, err := url.Parse(dsn.Source)
	if err != nil || u.Host == "" {
		return hidden
	}
	return "postgres: " + u.Host + "/" + strings.TrimPrefix(u.Path, "/")
}
