package admin

import "github.com/angvp/tango"

// ContentSecurityPolicy returns the policy the admin's own pages satisfy. The
// admin sends no policy by default. A host opts in by composing it into the
// admin tier:
//
//	admin.New(store, admin.WithMiddleware(
//		tango.SecurityHeaders(tango.WithContentSecurityPolicy(admin.ContentSecurityPolicy())),
//	))
//
// A host whose branding names a logo on another origin adds that origin to
// ImgSrc before composing the policy. Each call returns a new value, so such
// a change does not reach other callers.
func ContentSecurityPolicy() tango.CSP {
	return tango.CSP{
		DefaultSrc:     []tango.Source{tango.Self()},
		ScriptSrc:      []tango.Source{tango.Self()},
		StyleSrc:       []tango.Source{tango.Self()},
		ImgSrc:         []tango.Source{tango.Self()},
		FormAction:     []tango.Source{tango.Self()},
		ObjectSrc:      []tango.Source{tango.None()},
		BaseURI:        []tango.Source{tango.Self()},
		FrameAncestors: []tango.Source{tango.None()},
	}
}
