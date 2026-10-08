// reusable-greetings-host is the Host project for this example pair: it
// imports the "reusable-greetings" module by its real module path (see the
// require/replace in go.mod, standing in for a real published version) and
// installs its App into InstalledApps alongside a local app it scaffolded
// itself. No internals of the reusable app are copied here.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/angvp/tango"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/migration"

	"reusable-greetings/greetings"
	greetingsmigrations "reusable-greetings/migrations"

	"reusable-greetings-host/apps/echo"
	"reusable-greetings-host/migrations"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// TANGO_DB_DSN picks the database (default sqlite://app.db), opened
	// with the busy timeout and foreign keys db.ParseDSN adds.
	dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		return err
	}
	sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	store := db.NewStore(sqlDB, dsn.Dialect)
	config := appConfig(store)

	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, allMigrations()); handled || err != nil {
		return err
	}

	// Ctrl-C or SIGTERM cancels ctx, and ServeContext shuts down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}

// appConfig is the whole host; the test builds the same one.
func appConfig(store *db.Store) tango.Config {
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{
		echo.App{},
		greetings.New(store),
		admin.New(store),
	}
	return config
}

// allMigrations is the reusable app's contributed migrations (shipped inside
// its own repo) followed by this host's own, in InstalledApps order, before
// anything is applied or reported — see docs/guides/reusable-apps.md.
func allMigrations() []migration.Migration {
	var all []migration.Migration
	all = append(all, greetingsmigrations.Migrations...)
	return append(all, migrations.Migrations...)
}
