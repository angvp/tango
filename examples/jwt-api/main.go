package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/angvp/tango"
	tangojwt "github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
)

var exampleSecret = []byte("example-only-secret-not-for-production-use")

func main() {
	os.Exit(run())
}

func run() int {
	issueSubject := flag.String("issue-token", "", "issue a development-only JWT for this subject and exit")
	check := flag.Bool("check", false, "validate app registration and exit")
	flag.Parse()

	service, err := newJWTService()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if *issueSubject != "" {
		token, err := service.Issue(*issueSubject, 15*time.Minute)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(token)
		return 0
	}
	if *check {
		if err := tango.Check(exampleConfig(service)); err != nil {
			fmt.Fprintln(os.Stderr, "check failed:", err)
			return 1
		}
		fmt.Println("check passed")
		return 0
	}

	// Ctrl-C or SIGTERM cancels ctx, and ServeContext shuts down gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	config := exampleConfig(service)
	fmt.Println("listening on", config.Addr)
	// The example has no database, so ServeContext gets none.
	if err := tango.ServeContext(ctx, config, nil, db.SQLite); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func newJWTService() (*tangojwt.Service, error) {
	return tangojwt.NewService(
		tangojwt.Key{ID: "example-v1", Secret: exampleSecret},
		nil,
		"jwt-api-example",
		"jwt-api-example",
	)
}

func buildHandler(service *tangojwt.Service) (http.Handler, error) {
	registry, err := tango.BuildRegistry(exampleConfig(service))
	if err != nil {
		return nil, err
	}
	if err := registry.RunRegistration(); err != nil {
		return nil, err
	}
	return registry.Routes().Handler()
}

func exampleConfig(service *tangojwt.Service) tango.Config {
	me := tangojwt.Require(func(ctx *tango.Context) error {
		claims, _ := tangojwt.FromContext(ctx.Context())
		return ctx.JSON(http.StatusOK, map[string]string{"subject": claims.Subject})
	})
	app := tango.NewApp("jwt-api", func(registry *tango.Registry) error {
		return registry.Routes().Include("/api/", tango.URLs{
			tango.Path(http.MethodGet, "/me", me, tango.Name("me")),
		}, tango.WithMiddleware(service.Middleware(tangojwt.BearerToken)))
	})
	// The address is TANGO_ADDR, else the PORT hosting platforms set, else :8000.
	config := tango.LoadConfigFromEnv(tango.WithPortFromEnv())
	config.InstalledApps = []tango.App{app}
	return config
}
