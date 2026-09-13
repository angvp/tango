package admin

// Branding lets an app replace admin's generic sidebar title with its own
// name and logo — the only two named customization slots admin exposes
// beyond field-level Widgets (see ADR 0014: best-effort, not a stable v0.1
// contract, and no access to any other part of admin's layout/template).
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
	branding Branding
}

// WithBranding sets the admin's brand name and logo. Omitting it — calling
// admin.New(store) with no options, as before this existed — leaves the
// sidebar showing tanGO's generic default, unchanged.
func WithBranding(b Branding) Option {
	return func(c *adminConfig) { c.branding = b }
}
