// Command api-with-admin is a JSON API and the admin over one set of
// models: authors (admin only) and posts (admin and a JSON API), linked by
// a foreign key.
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
	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/observability"

	"api-with-admin/apps/authors"
	"api-with-admin/apps/locale"
	"api-with-admin/apps/posts"
	"api-with-admin/migrations"

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

	store := db.NewStore(sqlDB, dsn.Dialect)
	// The dashboard's activity numbers come from this Window, which also
	// receives every framework metric; Multi would fan out to an exporter too.
	window := observability.NewWindow()
	config := appConfig(store, sqlDB, dsn.Dialect, window)

	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations); handled || err != nil {
		return err
	}

	// Ctrl-C or SIGTERM cancels ctx, and ServeContext shuts down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect,
		tango.WithRecorder(observability.Multi(window)))
}

// appConfig is the whole application; the tests build the same one.
func appConfig(store *db.Store, sqlDB *sql.DB, dialect db.Dialect, window *observability.Window) tango.Config {
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	// The status provider needs the finished config (tango.Status checks
	// registration), so it reads it through this closure.
	var finished tango.Config
	dashboard := admin.DashboardConfig{
		Status:   dashboardStatus(func() tango.Config { return finished }, sqlDB, dialect),
		Activity: window,
	}
	// The admin and the Spanish catalogs: a request picks its language from
	// ?lang=, then the lang cookie, then Accept-Language (see apps/locale).
	registerCatalogs()
	config.Middleware = []tango.Middleware{i18n.MiddlewareResolver(locale.Resolver())}
	config.InstalledApps = []tango.App{
		posts.New(store),
		authors.New(),
		locale.New(),
		admin.New(store, admin.WithDashboard(dashboard)),
	}
	finished = config
	return config
}
