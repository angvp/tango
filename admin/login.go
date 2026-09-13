package admin

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

var loginTemplate = cloneWithContent(`{{define "content"}}
<div class="content-body">
  <div class="card max-w-md mx-auto">
    <div class="card-body">
      <h1 class="topbar-title mb-4">tanGO Admin</h1>
      {{if .Error}}<div class="alert-error">{{.Error}}</div>{{end}}
      <form method="post">
        <input type="hidden" name="next" value="{{.Next}}">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <div class="field-group">
          <label for="field-username" class="field-label">Username</label>
          <input id="field-username" type="text" name="username" value="{{.Username}}" class="input" autofocus>
        </div>
        <div class="field-group">
          <label for="field-password" class="field-label">Password</label>
          <input id="field-password" type="password" name="password" class="input">
        </div>
        <button type="submit" class="btn-primary w-full">Log in</button>
      </form>
    </div>
  </div>
</div>
{{end}}`)

type loginPageData struct {
	chrome
	Next      string
	Username  string
	Error     string
	CSRFToken string
}

// loginView handles GET (render the login form) and POST (verify
// credentials, create a session, redirect) for /admin/login/. limiter
// throttles repeated failed attempts from the same source IP.
func loginView(store *db.Store, limiter *loginRateLimiter) tango.View {
	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			token, err := ensureLoginCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, loginTemplate, loginPageData{Next: safeAdminNext(ctx.Query("next"), ""), CSRFToken: token})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyLoginCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}

			key := rateLimitKey(ctx.Request())
			if !limiter.Allow(key) {
				return tooManyLoginAttempts(ctx)
			}

			username := ctx.Request().PostForm.Get("username")
			password := ctx.Request().PostForm.Get("password")
			next := ctx.Request().PostForm.Get("next")
			csrfToken, _ := ctx.Request().Cookie(loginCSRFCookieName)

			user, ok, err := findAdminUserByUsername(ctx.Context(), store, username)
			if err != nil {
				return err
			}
			valid := ok && user.Active && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
			if !valid {
				limiter.RecordFailure(key)
				// Deliberately generic: never reveal whether the username
				// or the password was wrong, per this milestone's design.
				return render(ctx, http.StatusUnauthorized, loginTemplate, loginPageData{
					Next:      next,
					Username:  username,
					Error:     "Invalid username or password.",
					CSRFToken: csrfToken.Value,
				})
			}

			token, expiresAt, err := createSession(ctx.Context(), store, user.ID)
			if err != nil {
				return err
			}
			setSessionCookie(ctx.ResponseWriter(), ctx.Request(), token, expiresAt)

			target := "/admin/"
			target = safeAdminNext(next, target)
			return ctx.Redirect(target)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// logoutView handles POST for /admin/logout/: deletes the current
// session and clears its cookie.
func logoutView(store *db.Store) tango.View {
	return func(ctx *tango.Context) error {
		if ctx.Request().Method != http.MethodPost {
			return methodNotAllowed(ctx)
		}

		if cookie, err := ctx.Request().Cookie(sessionCookieName); err == nil {
			if err := deleteSessionByToken(ctx.Context(), store, cookie.Value); err != nil {
				return err
			}
		}
		clearSessionCookie(ctx.ResponseWriter(), ctx.Request())
		return ctx.Redirect("/admin/login/")
	}
}
