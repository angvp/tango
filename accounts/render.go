package accounts

import (
	"context"
	"html/template"
	"net/http"
	"strings"

	"github.com/angvp/tango"
	"github.com/angvp/tango/i18n"
)

// registerTemplate is deliberately minimal, plain HTML — this milestone
// proves the flow works, not tanGO's public-site design language. A real
// project is expected to replace or skin it immediately; there is no
// template-override hook (write your own view instead).
var registerTemplate = template.Must(template.New("register").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.register.title" "Register"}}</title></head>
<body>
  <h1>{{.T "accounts.register.heading" "Create an account"}}</h1>
  {{if .Data.Error}}<p style="color:red">{{.Data.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="next" value="{{.Data.Next}}">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <div>
      <label for="field-email">{{.T "accounts.field.email" "Email"}}</label>
      <input id="field-email" type="email" name="email" value="{{.Data.Email}}" autofocus>
    </div>
    <div>
      <label for="field-password">{{.T "accounts.field.password" "Password"}}</label>
      <input id="field-password" type="password" name="password">
    </div>
    <button type="submit">{{.T "accounts.register.submit" "Register"}}</button>
  </form>
  <p><a href="/accounts/login/">{{.T "accounts.register.login_link" "Already have an account? Log in"}}</a></p>
</body>
</html>`))

// registrationClosedTemplate is rendered instead of the form when signup
// is disabled — a clear, deliberate message, never a bare 404.
var registrationClosedTemplate = template.Must(template.New("registration_closed").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.register.closed_title" "Registration closed"}}</title></head>
<body>
  <h1>{{.T "accounts.register.closed" "Registration is currently closed."}}</h1>
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
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.login.title" "Log in"}}</title></head>
<body>
  <h1>{{.T "accounts.login.title" "Log in"}}</h1>
  {{if .Data.Error}}<p style="color:red">{{.Data.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="next" value="{{.Data.Next}}">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <div>
      <label for="field-email">{{.T "accounts.field.email" "Email"}}</label>
      <input id="field-email" type="email" name="email" value="{{.Data.Email}}" autofocus>
    </div>
    <div>
      <label for="field-password">{{.T "accounts.field.password" "Password"}}</label>
      <input id="field-password" type="password" name="password">
    </div>
    <button type="submit">{{.T "accounts.login.submit" "Log in"}}</button>
  </form>
  {{if .Data.SignupEnabled}}<p><a href="/accounts/register/">{{.T "accounts.login.register_link" "Need an account? Register"}}</a></p>{{end}}
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
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.reset.title" "Reset your password"}}</title></head>
<body>
  <h1>{{.T "accounts.reset.title" "Reset your password"}}</h1>
  {{if .Data.Error}}<p style="color:red">{{.Data.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <div>
      <label for="field-email">{{.T "accounts.field.email" "Email"}}</label>
      <input id="field-email" type="email" name="email" autofocus>
    </div>
    <button type="submit">{{.T "accounts.reset.submit" "Email me a link"}}</button>
  </form>
  <p><a href="/accounts/login/">{{.T "accounts.link.back_to_login" "Back to log in"}}</a></p>
</body>
</html>`))

// passwordResetSentTemplate is the one answer to every reset request, so
// it never says whether the address has an account.
var passwordResetSentTemplate = template.Must(template.New("password_reset_sent").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.check_email.title" "Check your email"}}</title></head>
<body>
  <h1>{{.T "accounts.check_email.title" "Check your email"}}</h1>
  <p>{{.T "accounts.reset_sent.body" "If an account exists for that address, we've sent it a link to reset its password. The link works for 1 hour."}}</p>
  <p><a href="/accounts/login/">{{.T "accounts.link.back_to_login" "Back to log in"}}</a></p>
</body>
</html>`))

// passwordResetConfirmTemplate asks for the new password. The form posts
// back to the same URL, token included.
var passwordResetConfirmTemplate = template.Must(template.New("password_reset_confirm").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.reset_confirm.title" "Choose a new password"}}</title></head>
<body>
  <h1>{{.T "accounts.reset_confirm.title" "Choose a new password"}}</h1>
  {{if .Data.Error}}<p style="color:red">{{.Data.Error}}</p>{{end}}
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <div>
      <label for="field-password">{{.T "accounts.field.new_password" "New password"}}</label>
      <input id="field-password" type="password" name="password" autocomplete="new-password" autofocus>
    </div>
    <button type="submit">{{.T "accounts.reset_confirm.submit" "Set password"}}</button>
  </form>
</body>
</html>`))

// invalidLinkTemplate answers every unusable emailed link the same way.
var invalidLinkTemplate = template.Must(template.New("invalid_link").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.invalid_link.title" "Link not valid"}}</title></head>
<body>
  <h1>{{.T "accounts.invalid_link.heading" "This link is not valid"}}</h1>
  <p>{{.T "accounts.invalid_link.body" "It may have expired, been used already, or been replaced by a newer one."}}</p>
  <p><a href="/accounts/login/">{{.T "accounts.link.back_to_login" "Back to log in"}}</a></p>
</body>
</html>`))

// verifyTemplate asks the person to confirm, so following the link alone
// verifies nothing.
var verifyTemplate = template.Must(template.New("verify").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.verify.title" "Confirm your email"}}</title></head>
<body>
  <h1>{{.T "accounts.verify.title" "Confirm your email"}}</h1>
  <form method="post">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <button type="submit">{{.T "accounts.verify.submit" "Confirm my email"}}</button>
  </form>
</body>
</html>`))

// verifiedTemplate answers a confirmed verification link.
var verifiedTemplate = template.Must(template.New("verified").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.verified.title" "Email confirmed"}}</title></head>
<body>
  <h1>{{.T "accounts.verified.heading" "Your email address is confirmed."}}</h1>
</body>
</html>`))

// verificationSentTemplate answers every resend the same way.
var verificationSentTemplate = template.Must(template.New("verification_sent").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.check_email.title" "Check your email"}}</title></head>
<body>
  <h1>{{.T "accounts.check_email.title" "Check your email"}}</h1>
  <p>{{.T "accounts.verification_sent.body" "If your address still needs confirming, we've sent a new link. Only the newest link works, for 24 hours."}}</p>
</body>
</html>`))

// unverifiedTemplate is RequireVerified's 403 page.
var unverifiedTemplate = template.Must(template.New("unverified").Parse(`<!doctype html>
<html{{with .Lang}} lang="{{.}}"{{end}}>
<head><meta charset="utf-8"><title>{{.T "accounts.verify.title" "Confirm your email"}}</title></head>
<body>
  <h1>{{.T "accounts.unverified.heading" "Please confirm your email address"}}</h1>
  <p>{{.T "accounts.unverified.body" "This page needs a confirmed email address. Open the link we emailed you, or ask for a new one."}}</p>
  <form method="post" action="/accounts/verify/resend/">
    <input type="hidden" name="csrf_token" value="{{.Data.CSRFToken}}">
    <button type="submit">{{.T "accounts.unverified.submit" "Send a new link"}}</button>
  </form>
</body>
</html>`))

// pageData is what every accounts page renders: the handler's data under
// Data, plus the request's translations.
type pageData struct {
	Data any
	ctx  context.Context
}

// T translates key for the page's request, for use in HTML text only (an
// element's content, never an attribute). The result is escaped here the way
// a literal in the template would be, which leaves an apostrophe as typed, so
// a page with nothing wired renders exactly the text it always did. A test
// pins that no accounts template uses it inside an attribute.
func (p pageData) T(key, fallback string, args ...any) template.HTML {
	return template.HTML(textEscaper.Replace(i18n.T(p.ctx, key, fallback, args...)))
}

var textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;")

// Lang is the registered locale of the catalog the request selected, or ""
// when none was, in which case the page declares no language.
func (p pageData) Lang() string {
	return selectedLocale(p.ctx)
}

func render(ctx *tango.Context, status int, tmpl *template.Template, data any) error {
	return ctx.HTML(status, tmpl, tmpl.Name(), pageData{Data: data, ctx: ctx.Context()})
}

func methodNotAllowed(ctx *tango.Context) error {
	return ctx.JSON(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
