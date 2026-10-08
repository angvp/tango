// Command accounts-mail shows password reset and email verification with
// the accounts app: emails are printed to the terminal by
// mail.WriterSender instead of being sent, so the whole flow runs locally
// with no mail server.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/mail"

	"accounts-mail/migrations"

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

	// Development only: every email, with its live link, is printed here.
	// A real deployment passes mail.SMTPSenderFromEnv() instead.
	addr := tango.LoadConfigFromEnv(tango.WithPortFromEnv()).Addr
	config := appConfig(db.NewStore(sqlDB, dsn.Dialect), mail.WriterSender(os.Stdout), baseURL(addr))
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations); handled || err != nil {
		return err
	}

	// Ctrl-C or SIGTERM cancels ctx, and ServeContext shuts down gracefully,
	// sending what's still queued in the accounts outbox first.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}

// baseURL is the Public base URL emailed links are built on: BASE_URL, or
// for local development http://localhost on the port the app listens on
// (addr, such as ":8000").
func baseURL(addr string) string {
	if url := os.Getenv("BASE_URL"); url != "" {
		return url
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "8000"
	}
	return "http://localhost:" + port
}

// appConfig is the whole application, sending its emails through sender.
// The test builds the same one with a sender that records them.
func appConfig(store *db.Store, sender mail.Sender, baseURL string) tango.Config {
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{
		accounts.New(store, accounts.WithMail(accounts.MailConfig{
			Sender:  sender,
			From:    "Example <noreply@localhost>",
			BaseURL: baseURL,
		})),
		tango.NewApp("home", func(registry *tango.Registry) error {
			return registry.Routes().Include("/", tango.URLs{
				tango.Path(http.MethodGet, "/", accounts.RequireVerified(store, accounts.DefaultSessionCookieName, "/accounts/login/", home(store)), tango.Name("home")),
			})
		}),
	}
	return config
}

// home greets an account whose email is verified; RequireVerified keeps
// everyone else out.
func home(store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		account, _, err := accounts.CurrentAccount(ctx, store, accounts.DefaultSessionCookieName)
		if err != nil {
			return err
		}
		return ctx.JSON(http.StatusOK, map[string]string{"message": "Hello, " + account.Email + ": your email is verified."})
	}
}
