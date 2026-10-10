// Command uploads is a small document locker: signed-in accounts upload
// files, list them, download them again and delete them. It shows the
// whole path tanGO's storage package expects a host to write: a View reads
// one multipart file with storage.Upload, the host keeps the generated key
// and metadata in its own model (inside a transaction), authorizes every
// download itself, and delivers it with storage.Serve.
//
// Files are kept on the local disk under UPLOADS_DIR, which suits
// development and a single host with a persistent volume. For more than one
// instance, or a disk that does not outlive the container, swap
// storage/local for storage/s3 by configuration; nothing else changes.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/storage"
	"github.com/angvp/tango/storage/local"

	"uploads/apps/documents"
	"uploads/migrations"

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

	objects, err := local.New(uploadsDir())
	if err != nil {
		return err
	}
	defer objects.Close()

	config := appConfig(db.NewStore(sqlDB, dsn.Dialect), objects)
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations); handled || err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}

// uploadsDir is UPLOADS_DIR, else ./uploads.
func uploadsDir() string {
	if dir := os.Getenv("UPLOADS_DIR"); dir != "" {
		return dir
	}
	return "uploads"
}

// appConfig is the whole application, keeping object bytes in objects.
// The test builds the same one around an in-memory store.
func appConfig(store *db.Store, objects storage.Store) tango.Config {
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{
		accounts.New(store),
		documents.New(store, objects, documents.DefaultLimits()),
	}
	return config
}
