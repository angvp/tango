package admin

import (
	"net"

	"github.com/angvp/tango"
)

// Branding lets an app replace admin's generic sidebar title with its own
// name and logo — the only two named customization slots admin exposes
// beyond field-level Widgets. This surface is best-effort, not a stable
// contract, and grants no access to any other part of admin's
// layout/template.
type Branding struct {
	// Name replaces the sidebar's generic "tanGO Admin" title when set.
	Name string
	// LogoURL replaces the sidebar's generic brand mark with an <img> when
	// set — typically a URL the app already serves via its own embed.FS
	// route.
	LogoURL string
}

// Option configures admin.New.
type Option func(*adminConfig)

type adminConfig struct {
	branding       Branding
	middleware     []tango.Middleware
	trustedProxies []*net.IPNet
}

// WithBranding sets the admin's brand name and logo. Omitting it — calling
// admin.New(store) with no options, as before this existed — leaves the
// sidebar showing tanGO's generic default, unchanged.
func WithBranding(b Branding) Option {
	return func(c *adminConfig) { c.branding = b }
}

// WithMiddleware wraps every admin route with host-provided Middleware.
// It is scoped to admin's internal /admin/ route group; global
// tango.Config.Middleware still wraps outside it.
func WithMiddleware(middleware ...tango.Middleware) Option {
	return func(c *adminConfig) {
		c.middleware = append(c.middleware, middleware...)
	}
}

// WithTrustedProxies tells the login limiter which reverse proxies sit in
// front of the application. Without it the limiter counts failed logins per
// connection address and never reads a forwarded header, which is right for
// a direct deployment. Behind a proxy that address is the proxy's, so every
// user would share one limit; naming the proxy's network here makes the
// limiter use the client address the proxy forwards, and only for requests
// whose connection comes from one of these networks. A header from any other
// peer is ignored. This affects the login limiter and nothing else.
func WithTrustedProxies(proxies ...*net.IPNet) Option {
	return func(c *adminConfig) { c.trustedProxies = append(c.trustedProxies, proxies...) }
}
