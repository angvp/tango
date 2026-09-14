package accounts

import (
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// loginRateLimitAttempts and loginRateLimitWindow mirror admin's own
// login limiter's tuning — a separate instance from registration's, since
// the two endpoints have different failure semantics.
const (
	loginRateLimitAttempts = 5
	loginRateLimitWindow   = time.Minute
)

// genericLoginError is returned for any login failure — an unknown email,
// a wrong password, or an inactive account are all indistinguishable, so
// an attacker learns nothing about which emails are registered. See ADR
// 0021; this is deliberately asymmetric with registration's specific
// duplicate-email error.
const genericLoginError = "Invalid email or password."

// loginView handles GET (render the login form) and POST (verify
// credentials, create a session, redirect) for /accounts/login/.
func loginView(store *db.Store, cfg accountsConfig, limiter *security.RateLimiter) tango.View {
	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			token, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, loginTemplate, loginPageData{
				Next:          safeAccountsNext(ctx.Query("next"), ""),
				CSRFToken:     token,
				SignupEnabled: !cfg.signupDisabled,
			})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyPreSessionCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}

			key := rateLimitKey(ctx.Request())
			if !limiter.Allow(key) {
				return tooManyRequests(ctx)
			}

			email := normalizeEmail(ctx.Request().PostForm.Get("email"))
			password := ctx.Request().PostForm.Get("password")
			next := ctx.Request().PostForm.Get("next")
			csrfCookie, _ := ctx.Request().Cookie(preSessionCSRFCookieName)

			account, exists, err := findAccountByEmail(ctx.Context(), store, email)
			if err != nil {
				return err
			}
			valid := exists && account.Active && auth.VerifyPassword(account.PasswordHash, password)
			if !valid {
				limiter.RecordFailure(key)
				return render(ctx, http.StatusUnauthorized, loginTemplate, loginPageData{
					Next:          next,
					Email:         email,
					Error:         genericLoginError,
					CSRFToken:     csrfCookie.Value,
					SignupEnabled: !cfg.signupDisabled,
				})
			}

			if err := createAccountSession(ctx.Context(), store, cfg, ctx.ResponseWriter(), ctx.Request(), account.ID); err != nil {
				return err
			}
			return ctx.Redirect(safeAccountsNext(next, defaultPostLoginRedirect))

		default:
			return methodNotAllowed(ctx)
		}
	}
}
