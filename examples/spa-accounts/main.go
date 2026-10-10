// Command spa-accounts is the backend of a single-page app: the accounts
// app's JSON mode gives a separate client registration, login, password
// reset and email verification with bearer tokens, no cookies. A host
// profiles app stores a username and display name in the same transaction
// that creates the account, and login accepts either the email or the
// username. Emails are printed to the terminal instead of sent.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	tangojwt "github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/mail"

	"spa-accounts/apps/profiles"
	"spa-accounts/migrations"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// settings is what differs between a deployment and a test.
type settings struct {
	// BaseURL is the origin of this API; accounts' own pages and mail use it.
	BaseURL string
	// ClientURL is the origin of the single-page app, which serves the
	// /verify and /reset pages the emailed links open.
	ClientURL string
	// JWTSecret signs access tokens: at least 32 bytes, from the environment.
	JWTSecret []byte
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

	addr := tango.LoadConfigFromEnv(tango.WithPortFromEnv()).Addr
	config, err := appConfig(db.NewStore(sqlDB, dsn.Dialect), mail.WriterSender(os.Stdout), settingsFromEnv(addr))
	if err != nil {
		return err
	}
	if handled, err := tango.DispatchFlags(config, sqlDB, dsn.Dialect, migrations.Migrations); handled || err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Println("listening on", config.Addr)
	return tango.ServeContext(ctx, config, sqlDB, dsn.Dialect)
}

// settingsFromEnv reads SPA_JWT_SECRET (required), BASE_URL (default
// http://localhost on the listening port) and CLIENT_URL (default
// http://localhost:3000, where a Next.js dev server runs).
func settingsFromEnv(addr string) settings {
	base := os.Getenv("BASE_URL")
	if base == "" {
		_, port, err := net.SplitHostPort(addr)
		if err != nil || port == "" {
			port = "8000"
		}
		base = "http://localhost:" + port
	}
	client := os.Getenv("CLIENT_URL")
	if client == "" {
		client = "http://localhost:3000"
	}
	return settings{BaseURL: base, ClientURL: client, JWTSecret: []byte(os.Getenv("SPA_JWT_SECRET"))}
}

// appConfig is the whole application, sending its emails through sender.
// The test builds the same one with a sender that records them.
func appConfig(store *db.Store, sender mail.Sender, s settings) (tango.Config, error) {
	if len(s.JWTSecret) < tangojwt.MinimumSecretBytes {
		return tango.Config{}, fmt.Errorf("SPA_JWT_SECRET must be set to at least %d bytes", tangojwt.MinimumSecretBytes)
	}
	service, err := tangojwt.NewService(tangojwt.Key{ID: "k1", Secret: s.JWTSecret}, nil, s.BaseURL, "spa-client")
	if err != nil {
		return tango.Config{}, err
	}

	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.Middleware = []tango.Middleware{cors(s.ClientURL)}
	config.MiddlewareScope = tango.MiddlewareScopeAll
	config.InstalledApps = []tango.App{
		accounts.New(store,
			accounts.WithMail(accounts.MailConfig{Sender: sender, From: "Example <noreply@localhost>", BaseURL: s.BaseURL}),
			accounts.WithJSON(accounts.JSONConfig{
				Auth: bearerAuth{service: service},
				// The links open pages of the single-page app. A token in
				// the fragment never reaches a server log or a Referer.
				VerifyURL:         s.ClientURL + "/verify#token={token}",
				ResetURL:          s.ClientURL + "/reset#token={token}",
				OnRegister:        profiles.OnRegister,
				ResolveIdentifier: profiles.ResolveUsername,
			}),
		),
		profiles.New(),
		apiApp(store, service),
	}
	return config, nil
}

// apiApp is the application's own protected route: who the token names.
func apiApp(store *db.Store, service *tangojwt.Service) tango.App {
	return tango.NewApp("api", func(registry *tango.Registry) error {
		accountMeta, ok := registry.Models().Get("Account")
		if !ok {
			return errors.New("api: install accounts before this app")
		}
		me := tangojwt.Require(func(ctx *tango.Context) error {
			claims, _ := tangojwt.FromContext(ctx.Context())
			var account accounts.Account
			id, err := strconv.ParseInt(claims.Subject, 10, 64)
			if err != nil {
				return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			if err := store.Get(ctx.Context(), accountMeta, id, &account); err != nil {
				if errors.Is(err, db.ErrNotFound) {
					return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				}
				return err
			}
			profile, _, err := profiles.ForAccount(ctx.Context(), store, account.ID)
			if err != nil {
				return err
			}
			return ctx.JSON(http.StatusOK, map[string]any{
				"id": account.ID, "email": account.Email, "email_verified": !account.EmailVerifiedAt.IsZero(),
				"username": profile.Username, "display_name": profile.DisplayName, "bio": profile.Bio,
			})
		})
		return registry.Routes().Include("/api/", tango.URLs{
			tango.Path(http.MethodGet, "/me/", me, tango.Name("me")),
		}, tango.WithMiddleware(service.Middleware(tangojwt.BearerToken)))
	})
}
