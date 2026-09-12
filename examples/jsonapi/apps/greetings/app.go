package greetings

import "github.com/angvp/tango"

// App implements tango.App for the "greetings" app.
type App struct{}

func (App) Name() string {
	return "greetings"
}

func (App) Register(registry *tango.Registry) error {
	return registry.Routes().Include("/greetings/", tango.URLs{
		tango.Path("GET", "/{name}/", hello, tango.Name("hello")),
	})
}

func hello(ctx *tango.Context) error {
	name := ctx.Param("name")
	return ctx.JSON(200, map[string]string{
		"message": "Hello, " + name + "!",
	})
}
