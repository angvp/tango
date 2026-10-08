// Command jsonapi is the smallest JSON application: one hand-written app,
// installed in main.go, answering one named route.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/angvp/tango"

	"jsonapi/apps/greetings"
	"jsonapi/migrations"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dsn, err := tango.LoadDBConfigFromEnv()
	if err != nil {
		return err
	}
	sqlDB, err := sql.Open(dsn.Driver, dsn.Source)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	config := appConfig()
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations); handled || err != nil {
		return err
	}

	// Ctrl-C or SIGTERM cancels ctx, and ServeContext shuts down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}

// appConfig is the whole application; the test builds the same one. An app
// is installed by listing it here: tango newapp never edits this file.
func appConfig() tango.Config {
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{greetings.App{}}
	return config
}
