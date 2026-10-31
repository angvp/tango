package tango

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SecurityHeaders returns middleware that sets a Content-Security-Policy and
// the companion security headers on every response it wraps. Installed with
// no options it applies the strict baseline policy.
//
// The middleware sets its headers rather than adding them, so an inner
// install replaces an outer one: the innermost policy is the only one sent.
// A policy that uses NonceSource gets a fresh nonce for each request, which
// the handler reads with CSPNonce.
func SecurityHeaders(opts ...SecurityHeaderOption) Middleware {
	config := securityHeadersConfig{
		policy:         StrictCSP(),
		referrerPolicy: defaultReferrerPolicy,
	}
	for _, opt := range opts {
		opt(&config)
	}
	usesNonce := config.policy.usesNonce()
	staticPolicy := ""
	if !usesNonce {
		staticPolicy = config.policy.render("")
	}
	frameOptions, frameOK := frameOptionsFor(config.policy.FrameAncestors)
	hsts := ""
	if config.hstsMaxAge > 0 {
		hsts = "max-age=" + strconv.FormatInt(int64(config.hstsMaxAge/time.Second), 10)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			nonce := ""
			policy := staticPolicy
			if usesNonce {
				generated, err := generateNonce()
				if err != nil {
					writeInternalError(w)
					return
				}
				nonce = generated
				policy = config.policy.render(nonce)
			}
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", config.referrerPolicy)
			h.Set("Content-Security-Policy", policy)
			if frameOK {
				h.Set("X-Frame-Options", frameOptions)
			} else {
				h.Del("X-Frame-Options")
			}
			if hsts != "" && r.TLS != nil {
				h.Set("Strict-Transport-Security", hsts)
			}
			r = r.WithContext(context.WithValue(r.Context(), cspNonceKey{}, nonce))
			next.ServeHTTP(w, r)
		})
	}
}

const defaultReferrerPolicy = "strict-origin-when-cross-origin"

type securityHeadersConfig struct {
	policy         CSP
	referrerPolicy string
	hstsMaxAge     time.Duration
}

// SecurityHeaderOption configures SecurityHeaders.
type SecurityHeaderOption func(*securityHeadersConfig)

// WithContentSecurityPolicy sets the policy the middleware sends. The policy
// is used exactly as written: it is not merged with the strict baseline. It
// panics when the policy has no directives, because an empty policy would send
// no Content-Security-Policy at all while the host believes one is applied.
func WithContentSecurityPolicy(policy CSP) SecurityHeaderOption {
	if policy.render("") == "" {
		panic("tango: Content-Security-Policy has no directives")
	}
	return func(config *securityHeadersConfig) {
		config.policy = cloneCSP(policy)
	}
}

// WithHSTS enables Strict-Transport-Security with the given max-age. The
// header is sent only on requests that arrived over TLS, and never carries
// includeSubDomains or preload. It panics when maxAge is under one second.
func WithHSTS(maxAge time.Duration) SecurityHeaderOption {
	if maxAge < time.Second {
		panic("tango: HSTS max-age must be at least one second")
	}
	return func(config *securityHeadersConfig) {
		config.hstsMaxAge = maxAge
	}
}

// WithReferrerPolicy sets the Referrer-Policy value the middleware sends. It
// panics unless value is one of the standard Referrer-Policy values.
func WithReferrerPolicy(value string) SecurityHeaderOption {
	if !standardReferrerPolicies[value] {
		panic("tango: invalid Referrer-Policy " + value)
	}
	return func(config *securityHeadersConfig) {
		config.referrerPolicy = value
	}
}

var standardReferrerPolicies = map[string]bool{
	"no-referrer":                     true,
	"no-referrer-when-downgrade":      true,
	"origin":                          true,
	"origin-when-cross-origin":        true,
	"same-origin":                     true,
	"strict-origin":                   true,
	"strict-origin-when-cross-origin": true,
	"unsafe-url":                      true,
}

func cloneCSP(p CSP) CSP {
	return CSP{
		DefaultSrc:     append([]Source(nil), p.DefaultSrc...),
		ScriptSrc:      append([]Source(nil), p.ScriptSrc...),
		StyleSrc:       append([]Source(nil), p.StyleSrc...),
		ImgSrc:         append([]Source(nil), p.ImgSrc...),
		FontSrc:        append([]Source(nil), p.FontSrc...),
		ConnectSrc:     append([]Source(nil), p.ConnectSrc...),
		FormAction:     append([]Source(nil), p.FormAction...),
		ObjectSrc:      append([]Source(nil), p.ObjectSrc...),
		BaseURI:        append([]Source(nil), p.BaseURI...),
		FrameAncestors: append([]Source(nil), p.FrameAncestors...),
	}
}

// CSP is a Content-Security-Policy built from typed directive values. The
// package alone renders it to a header string, in one fixed directive order.
type CSP struct {
	DefaultSrc     []Source
	ScriptSrc      []Source
	StyleSrc       []Source
	ImgSrc         []Source
	FontSrc        []Source
	ConnectSrc     []Source
	FormAction     []Source
	ObjectSrc      []Source
	BaseURI        []Source
	FrameAncestors []Source
}

// StrictCSP returns the baseline policy that SecurityHeaders applies when no
// policy option is given. Each call returns a new value, so a caller may
// modify its copy without changing the middleware's baseline.
func StrictCSP() CSP {
	return CSP{
		DefaultSrc:     []Source{Self()},
		ScriptSrc:      []Source{Self(), NonceSource()},
		StyleSrc:       []Source{Self(), NonceSource()},
		ObjectSrc:      []Source{None()},
		BaseURI:        []Source{Self()},
		FrameAncestors: []Source{Self()},
	}
}

type cspDirective struct {
	name    string
	sources []Source
}

// directives lists the policy's directives in the order they are rendered.
func (p CSP) directives() []cspDirective {
	return []cspDirective{
		{"default-src", p.DefaultSrc},
		{"script-src", p.ScriptSrc},
		{"style-src", p.StyleSrc},
		{"img-src", p.ImgSrc},
		{"font-src", p.FontSrc},
		{"connect-src", p.ConnectSrc},
		{"form-action", p.FormAction},
		{"object-src", p.ObjectSrc},
		{"base-uri", p.BaseURI},
		{"frame-ancestors", p.FrameAncestors},
	}
}

// usesNonce reports whether any directive carries NonceSource.
func (p CSP) usesNonce() bool {
	for _, d := range p.directives() {
		for _, s := range d.sources {
			if s == NonceSource() {
				return true
			}
		}
	}
	return false
}

// render returns the header value for the policy, with nonce as the value of
// every NonceSource. Directives with no sources are omitted.
func (p CSP) render(nonce string) string {
	var parts []string
	for _, d := range p.directives() {
		if len(d.sources) == 0 {
			continue
		}
		tokens := []string{d.name}
		for _, s := range d.sources {
			tokens = append(tokens, s.render(nonce))
		}
		parts = append(parts, strings.Join(tokens, " "))
	}
	return strings.Join(parts, "; ")
}

// frameOptionsFor derives the legacy X-Frame-Options value from the
// frame-ancestors sources. It reports false when the legacy header cannot say
// the same thing.
func frameOptionsFor(ancestors []Source) (string, bool) {
	switch {
	case len(ancestors) == 0, len(ancestors) == 1 && ancestors[0] == None():
		return "DENY", true
	case len(ancestors) == 1 && ancestors[0] == Self():
		return "SAMEORIGIN", true
	}
	return "", false
}

type sourceKind int

const (
	sourceSelf sourceKind = iota + 1
	sourceNone
	sourceHost
	sourceScheme
	sourceData
	sourceBlob
	sourceUnsafeInlineStyle
	sourceNonce
)

// Source is one entry of a CSP directive. Its zero value is not a valid
// source; build one with the constructors in this package.
type Source struct {
	kind  sourceKind
	value string
}

// schemePattern is the grammar of a URL scheme as CSP accepts it in a
// scheme-source.
var schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)

// Host is a host-source: a host name, optionally with a scheme, port or path
// (for example "cdn.example.com" or "https://cdn.example.com:8443").
// It panics when name contains whitespace, a quote, a backslash, a comma or
// a semicolon, or any character outside printable ASCII, because such a name
// could add directives or sources to the header.
func Host(name string) Source {
	if !validSourceToken(name) {
		panic("tango: invalid Content-Security-Policy host " + name)
	}
	return Source{kind: sourceHost, value: name}
}

// Scheme is a scheme-source such as "https". It panics when scheme is not a
// valid URL scheme.
func Scheme(scheme string) Source {
	if !schemePattern.MatchString(scheme) {
		panic("tango: invalid Content-Security-Policy scheme " + scheme)
	}
	return Source{kind: sourceScheme, value: scheme}
}

// Data is the data: scheme source.
func Data() Source { return Source{kind: sourceData} }

// Blob is the blob: scheme source.
func Blob() Source { return Source{kind: sourceBlob} }

// UnsafeInlineStyle is the 'unsafe-inline' source. It is valid in style-src
// only, and exists for the one route that needs inline styles. Placing it in
// another directive is a caller error the type does not prevent.
func UnsafeInlineStyle() Source { return Source{kind: sourceUnsafeInlineStyle} }

// NonceSource is the per-request nonce source. The middleware generates one
// nonce per request when the policy uses it, and CSPNonce returns it.
func NonceSource() Source { return Source{kind: sourceNonce} }

func validSourceToken(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if r <= 0x20 || r > 0x7e || strings.ContainsRune(`;,'"\`, r) {
			return false
		}
	}
	return true
}

// Self is the 'self' source: the application's own origin.
func Self() Source { return Source{kind: sourceSelf} }

// None is the 'none' source: no source at all.
func None() Source { return Source{kind: sourceNone} }

func (s Source) render(nonce string) string {
	switch s.kind {
	case sourceSelf:
		return "'self'"
	case sourceNone:
		return "'none'"
	case sourceHost:
		return s.value
	case sourceScheme:
		return s.value + ":"
	case sourceData:
		return "data:"
	case sourceBlob:
		return "blob:"
	case sourceUnsafeInlineStyle:
		return "'unsafe-inline'"
	case sourceNonce:
		return "'nonce-" + nonce + "'"
	}
	panic("tango: unknown Content-Security-Policy source")
}

// nonceRandomSource supplies the nonce bytes. Tests replace it through
// export_test.go; it is crypto/rand in every build.
var nonceRandomSource io.Reader = rand.Reader

// generateNonce returns 16 random bytes in standard base64.
func generateNonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := io.ReadFull(nonceRandomSource, bytes); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

type cspNonceKey struct{}

// CSPNonce returns the nonce the SecurityHeaders middleware generated for the
// request, or the empty string when no middleware generated one. Use it in a
// handler, or pass ctx.Context() to a template's data.
func CSPNonce(ctx context.Context) string {
	nonce, _ := ctx.Value(cspNonceKey{}).(string)
	return nonce
}

// NonceAttr renders a nonce as a leading-space nonce attribute for an HTML
// tag, as in <script{{NonceAttr .Nonce}}>. It renders nothing for an empty
// nonce, so a template works with or without the middleware.
func NonceAttr(nonce string) template.HTMLAttr {
	if nonce == "" {
		return ""
	}
	return template.HTMLAttr(` nonce="` + template.HTMLEscapeString(nonce) + `"`)
}
