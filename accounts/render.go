package accounts

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

// registerTemplate is deliberately minimal, plain HTML — this milestone
// proves the flow works, not tanGO's public-site design language. A real
// project is expected to replace or skin it immediately; there is no
// template-override hook in v0.0.1 (write your own view instead).
var registerTemplate = template.Must(template.New("register").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Register</title></head>
<body>
  <h1>Create an account</h1>
  {{if .Error}}<p style="color:red">{{.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="next" value="{{.Next}}">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div>
      <label for="field-email">Email</label>
      <input id="field-email" type="email" name="email" value="{{.Email}}" autofocus>
    </div>
    <div>
      <label for="field-password">Password</label>
      <input id="field-password" type="password" name="password">
    </div>
    <button type="submit">Register</button>
  </form>
  <p><a href="/accounts/login/">Already have an account? Log in</a></p>
</body>
</html>`))

// registrationClosedTemplate is rendered instead of the form when signup
// is disabled — a clear, deliberate message, never a bare 404.
var registrationClosedTemplate = template.Must(template.New("registration_closed").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Registration closed</title></head>
<body>
  <h1>Registration is currently closed.</h1>
</body>
</html>`))

type registerPageData struct {
	Next      string
	Email     string
	Error     string
	CSRFToken string
}

// loginTemplate is deliberately minimal, plain HTML for the same reasons
// registerTemplate is.
var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Log in</title></head>
<body>
  <h1>Log in</h1>
  {{if .Error}}<p style="color:red">{{.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="next" value="{{.Next}}">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div>
      <label for="field-email">Email</label>
      <input id="field-email" type="email" name="email" value="{{.Email}}" autofocus>
    </div>
    <div>
      <label for="field-password">Password</label>
      <input id="field-password" type="password" name="password">
    </div>
    <button type="submit">Log in</button>
  </form>
  {{if .SignupEnabled}}<p><a href="/accounts/register/">Need an account? Register</a></p>{{end}}
</body>
</html>`))

type loginPageData struct {
	Next          string
	Email         string
	Error         string
	CSRFToken     string
	SignupEnabled bool
}

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	return ctx.HTML(status, tmpl, tmpl.Name(), data)
}

func methodNotAllowed(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
