package accounts

import "time"

// WithClock replaces time.Now for the cooldown and token expiry, for tests.
func WithClock(now func() time.Time) Option {
	return func(c *accountsConfig) { c.now = now }
}

// WithOutboxCapacity replaces the outbox's capacity of 100, for tests.
func WithOutboxCapacity(n int) Option {
	return func(c *accountsConfig) { c.outboxCapacity = n }
}
