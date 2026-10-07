package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/admin"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"

	"board/apps/api"
	"board/apps/housekeeping"
	"board/apps/live"
	"board/apps/posts"
	"board/apps/web"
	"board/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// One structured JSON logger for the whole app. slog.SetDefault makes
	// it the logger for tanGO's middleware and for slog calls in your code.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := tango.LoadEnvFile(".env"); err != nil {
		return err
	}

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
	tokens, err := newTokenService()
	if err != nil {
		return err
	}
	feed, err := live.NewFeed()
	if err != nil {
		return err
	}

	config := appConfig(store, tokens, feed)

	if handled, err := admin.HandleCLI(context.Background(), store, os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled || err != nil {
		return err
	}

	handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations)
	if handled || err != nil {
		return err
	}

	// Ctrl-C or SIGTERM (what `docker stop` and most process managers send)
	// cancels ctx, and ServeContext shuts down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("listening", "addr", config.Addr)
	err = tango.ServeContext(ctx, config, sqlDB, dsn.Dialect,
		tango.WithLogger(logger),
		tango.WithShutdownTimeout(10*time.Second),
	)
	logger.Info("stopped", "clean", err == nil)
	return err
}

// newTokenService builds the API's token issuer from BOARD_JWT_SECRET.
func newTokenService() (*jwt.Service, error) {
	secret := os.Getenv("BOARD_JWT_SECRET")
	if len(secret) < jwt.MinimumSecretBytes {
		return nil, fmt.Errorf("BOARD_JWT_SECRET must be set to a random value of at least %d bytes", jwt.MinimumSecretBytes)
	}
	return jwt.NewService(
		jwt.Key{ID: "board-1", Secret: []byte(secret)},
		nil,
		"board",     // issuer
		"board-api", // audience
		jwt.WithMaxTTL(time.Hour),
	)
}

// appConfig is the whole application: which apps are installed, in which
// order, and the middleware around every request. The tests build the
// exact same config.
func appConfig(store *db.Store, tokens *jwt.Service, feed *live.Feed) tango.Config {
	config := tango.LoadConfigFromEnv() // Addr from TANGO_ADDR, default :8000
	config.InstalledApps = []tango.App{
		accounts.New(store),
		posts.New(store, tokens, feed),
		api.New(store, tokens),
		web.New(store, feed),
		feed.App(),
		housekeeping.New(store),
		admin.New(store),
	}
	config.Middleware = []tango.Middleware{
		tango.RequestID(),
		tango.Recoverer(),
		tango.AccessLogger(),
	}
	return config
}
