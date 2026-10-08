package accounts

import (
	"html/template"
	"net/http"

	"github.com/angvp/tango"
)

// registerTemplate is deliberately minimal, plain HTML — this milestone
// proves the flow works, not tanGO's public-site design language. A real
// project is expected to replace or skin it immediately; there is no
// template-override hook (write your own view instead).
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

// passwordResetRequestTemplate asks for the address to send a reset link
// to.
var passwordResetRequestTemplate = template.Must(template.New("password_reset_request").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Reset your password</title></head>
<body>
  <h1>Reset your password</h1>
  {{if .Error}}<p style="color:red">{{.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div>
      <label for="field-email">Email</label>
      <input id="field-email" type="email" name="email" autofocus>
    </div>
    <button type="submit">Email me a link</button>
  </form>
  <p><a href="/accounts/login/">Back to log in</a></p>
</body>
</html>`))

// passwordResetSentTemplate is the one answer to every reset request, so
// it never says whether the address has an account.
var passwordResetSentTemplate = template.Must(template.New("password_reset_sent").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Check your email</title></head>
<body>
  <h1>Check your email</h1>
  <p>If an account exists for that address, we've sent it a link to reset its password. The link works for 1 hour.</p>
  <p><a href="/accounts/login/">Back to log in</a></p>
</body>
</html>`))

// passwordResetConfirmTemplate asks for the new password. The form posts
// back to the same URL, token included.
var passwordResetConfirmTemplate = template.Must(template.New("password_reset_confirm").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Choose a new password</title></head>
<body>
  <h1>Choose a new password</h1>
  {{if .Error}}<p style="color:red">{{.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div>
      <label for="field-password">New password</label>
      <input id="field-password" type="password" name="password" autocomplete="new-password" autofocus>
    </div>
    <button type="submit">Set password</button>
  </form>
</body>
</html>`))

// invalidLinkTemplate answers every unusable emailed link the same way.
var invalidLinkTemplate = template.Must(template.New("invalid_link").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Link not valid</title></head>
<body>
  <h1>This link is not valid</h1>
  <p>It may have expired, been used already, or been replaced by a newer one.</p>
  <p><a href="/accounts/login/">Back to log in</a></p>
</body>
</html>`))

// verifyTemplate asks the person to confirm, so following the link alone
// verifies nothing.
var verifyTemplate = template.Must(template.New("verify").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Confirm your email</title></head>
<body>
  <h1>Confirm your email</h1>
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <button type="submit">Confirm my email</button>
  </form>
</body>
</html>`))

// verifiedTemplate answers a confirmed verification link.
var verifiedTemplate = template.Must(template.New("verified").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Email confirmed</title></head>
<body>
  <h1>Your email address is confirmed.</h1>
</body>
</html>`))

// verificationSentTemplate answers every resend the same way.
var verificationSentTemplate = template.Must(template.New("verification_sent").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Check your email</title></head>
<body>
  <h1>Check your email</h1>
  <p>If your address still needs confirming, we've sent a new link. Only the newest link works, for 24 hours.</p>
</body>
</html>`))

// unverifiedTemplate is RequireVerified's 403 page.
var unverifiedTemplate = template.Must(template.New("unverified").Parse(`<!doctype html>
<html>
<head><meta charset="utf-8"><title>Confirm your email</title></head>
<body>
  <h1>Please confirm your email address</h1>
  <p>This page needs a confirmed email address. Open the link we emailed you, or ask for a new one.</p>
  <form method="post" action="/accounts/verify/resend/">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <button type="submit">Send a new link</button>
  </form>
</body>
</html>`))

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	return ctx.HTML(status, tmpl, tmpl.Name(), data)
}

func methodNotAllowed(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
