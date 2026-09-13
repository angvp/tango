// Package echo is a small local app — scaffolded and owned by this host
// project, never imported elsewhere — installed alongside the reusable
// greetings app to prove a host can mix local and reusable apps freely.
package echo

import "github.com/angvp/tango"

type App struct{}

func (App) Name() string {
	return "echo"
}

func (App) Register(registry *tango.Registry) error {
	return registry.Routes().Include("/echo/", tango.URLs{
		tango.Path("GET", "/{message}/", echoMessage, tango.Name("message")),
	})
}

func echoMessage(ctx *tango.Context) error {
	return ctx.JSON(200, map[string]string{"echo": ctx.Param("message")})
}
