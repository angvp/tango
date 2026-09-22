package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/angvp/tango"
	tangojwt "github.com/angvp/tango/auth/jwt"
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

	handler, err := buildHandler(service)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("listening on :8000")
	if err := http.ListenAndServe(":8000", handler); err != nil {
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
	return tango.Config{InstalledApps: []tango.App{app}, Addr: ":8000"}
}
