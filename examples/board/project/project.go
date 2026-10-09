// Package project is the whole board application: which apps are installed,
// in which order, and the middleware around every request. The server
// (main.go), the shell (shell/main.go) and the tests all call Config, so they
// register exactly the same apps.
package project

import (
	"fmt"
	"log"
	"os"
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
)

// Config composes the application: its installed apps, middleware and
// ordinary configuration. It does not load the .env file or open a database:
// each process does that itself and passes the store in.
func Config(store *db.Store) tango.Config {
	// Config has no error to return, so a missing secret or a feed that
	// cannot start stops the process here, with the message, before
	// anything is served or opened.
	tokens, err := newTokenService()
	if err != nil {
		log.Fatal(err)
	}
	feed, err := live.NewFeed()
	if err != nil {
		log.Fatal(err)
	}

	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv()) // TANGO_ADDR, else PORT, else :8000
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
		tango.MaxBodySize(1 << 20),
	}
	config.MiddlewareScope = tango.MiddlewareScopeAll
	return config
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
