package mail

import (
	"crypto/x509"
	"time"
)

// WithRootCAs makes an SMTP sender trust pool, for tests against a server
// with a self-signed certificate.
func WithRootCAs(pool *x509.CertPool) Option {
	return func(o *options) { o.rootCAs = pool }
}

// WithDefaultTimeout replaces the 30-second default, for tests.
func WithDefaultTimeout(d time.Duration) Option {
	return func(o *options) { o.defaultTimeout = d }
}
