// Package accounts is tanGO's first-party, optional, installable reusable
// app: a ready-made HTML email/password register/login/logout flow, built
// by composing github.com/angvp/tango/auth's primitives rather than adding
// to them. auth stays primitive, unopinionated machinery; accounts is the
// batteries-included convenience layer on top for a project that wants a
// conventional account system without hand-rolling one. Installing it is
// optional and purely additive — a project with custom identity needs
// keeps using auth primitives directly and simply doesn't install this
// package. See CONTEXT.md's "Accounts app" entry.
package accounts

import (
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// registerRateLimitAttempts and registerRateLimitWindow bound how many
// failed registration attempts a single source IP may make before being
// throttled — the same shape as admin's login limiter, promoted to
// internal/security so both apps share one implementation.
const (
	registerRateLimitAttempts = 5
	registerRateLimitWindow   = time.Minute
)

// Option configures accounts.New.
type Option func(*accountsConfig)

// accountsConfig holds accounts.New's optional configuration.
type accountsConfig struct {
	signupDisabled    bool
	sessionDuration   time.Duration
	sessionCookieName string
}

// WithSignupDisabled opts an installation out of self-service registration.
// /accounts/register/ stays mounted and responds with a clear
// closed-registration message — never a bare 404 — while /accounts/login/
// remains fully available. Signup is enabled by default.
func WithSignupDisabled() Option {
	return func(c *accountsConfig) { c.signupDisabled = true }
}

// WithSessionDuration sets how long a created AccountSession stays valid,
// overriding the default (30 days — deliberately longer than admin's fixed
// 24-hour session, since public-user and operator expectations differ).
func WithSessionDuration(d time.Duration) Option {
	return func(c *accountsConfig) { c.sessionDuration = d }
}

// WithSessionCookieName overrides the session cookie's name, which
// defaults to "tango_account_session".
func WithSessionCookieName(name string) Option {
	return func(c *accountsConfig) { c.sessionCookieName = name }
}

// New constructs the accounts application. opts configures optional
// accounts-wide behavior; accounts.New(store) with no options is the
// baseline this milestone establishes and stays valid as later options are
// added.
func New(store *db.Store, opts ...Option) tango.App {
	cfg := accountsConfig{
		sessionDuration:   defaultSessionDuration,
		sessionCookieName: DefaultSessionCookieName,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	return tango.NewApp("accounts", func(registry *tango.Registry) error {
		registry.SetStore(store)

		if err := registry.Models().Register(Account{}); err != nil {
			return err
		}
		if err := registry.Models().Register(AccountSession{}); err != nil {
			return err
		}

		registerLimiter := security.NewRateLimiter(registerRateLimitAttempts, registerRateLimitWindow)
		loginLimiter := security.NewRateLimiter(loginRateLimitAttempts, loginRateLimitWindow)
		routes := tango.URLs{
			tango.Path(http.MethodGet, "/accounts/register/", registerView(store, cfg, registerLimiter)),
			tango.Path(http.MethodPost, "/accounts/register/", registerView(store, cfg, registerLimiter)),
			tango.Path(http.MethodGet, "/accounts/login/", loginView(store, cfg, loginLimiter)),
			tango.Path(http.MethodPost, "/accounts/login/", loginView(store, cfg, loginLimiter)),
		}

		return registry.Routes().Include("/", routes)
	})
}
