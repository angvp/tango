package accounts

import (
	"context"
	"fmt"
	"time"

	"github.com/angvp/tango/db"
)

// WithClock replaces time.Now for the cooldown and token expiry, for tests.
func WithClock(now func() time.Time) Option {
	return func(c *accountsConfig) { c.now = now }
}

// WithOutboxCapacity replaces the outbox's capacity of 100, for tests.
func WithOutboxCapacity(n int) Option {
	return func(c *accountsConfig) { c.outboxCapacity = n }
}

// UseTokenTwice stores a password-reset token for account and uses its row
// twice through the mailer's single-use step, reporting what each use got.
func UseTokenTwice(ctx context.Context, store *db.Store, account Account) (first, second bool, err error) {
	m := &mailer{store: store, cfg: accountsConfig{now: time.Now}}
	token, err := issueToken(ctx, store, account, PurposePasswordReset, time.Now(), time.Hour)
	if err != nil {
		return false, false, err
	}
	row, _, ok, err := m.usableToken(ctx, token, PurposePasswordReset)
	if err != nil || !ok {
		return false, false, fmt.Errorf("the new token is not usable: ok=%v err=%v", ok, err)
	}
	if first, err = m.useToken(ctx, row); err != nil {
		return false, false, err
	}
	second, err = m.useToken(ctx, row)
	return first, second, err
}

// IssueTokenFor issues a token of purpose for account, as the mailer does.
func IssueTokenFor(ctx context.Context, store *db.Store, account Account, purpose TokenPurpose) (string, error) {
	return issueToken(ctx, store, account, purpose, time.Now(), time.Hour)
}
