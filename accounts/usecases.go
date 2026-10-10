package accounts

import (
	"context"
	"sync"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
)

// The functions below are the account flows without any presentation: they
// take plain values and return typed results, and the HTML views (and any
// other adapter) own the request parsing, CSRF, rate limiting and rendering.
// Password rules, email normalization, the single-use token rules, the
// changed-email check and the Active check live here once.

// registrationOutcome is what registerAccount decided.
type registrationOutcome struct {
	Account Account
	// Problem is a user-facing reason the input cannot be used; the account
	// was not created.
	Problem string
	// Duplicate reports that the email is already registered.
	Duplicate bool
}

// created reports whether registerAccount created Account.
func (o registrationOutcome) created() bool { return o.Problem == "" && !o.Duplicate }

// registerAccount validates an already-normalized email and a password and,
// when both are usable, creates an active Account. Mail, sessions and grants
// are the caller's business, after this returns.
func registerAccount(ctx context.Context, store *db.Store, email, password string) (registrationOutcome, error) {
	if email == "" || password == "" {
		return registrationOutcome{Problem: "Email and password are required."}, nil
	}
	if problem := passwordProblem(password); problem != "" {
		return registrationOutcome{Problem: problem}, nil
	}
	account, created, err := createAccount(ctx, store, email, password)
	if err != nil {
		return registrationOutcome{}, err
	}
	if !created {
		return registrationOutcome{Duplicate: true}, nil
	}
	return registrationOutcome{Account: account}, nil
}

// authenticate returns the active Account for an already-normalized email
// whose password matches. Every failure (unknown email, inactive account,
// wrong password) is the same ok=false, so a caller cannot tell them apart.
func authenticate(ctx context.Context, store *db.Store, email, password string) (Account, bool, error) {
	account, exists, err := findAccountByEmail(ctx, store, email)
	if err != nil {
		return Account{}, false, err
	}
	if !exists {
		rejectUnknown(password)
		return Account{}, false, nil
	}
	if !checkPassword(account, password) {
		return Account{}, false, nil
	}
	return account, true, nil
}

// checkPassword reports whether account is active and password is its own.
// The password is compared even for an inactive account, so how long a
// refusal takes says nothing about why.
func checkPassword(account Account, password string) bool {
	matches := auth.VerifyPassword(account.PasswordHash, password)
	return account.Active && matches
}

var (
	unknownHashOnce sync.Once
	unknownHash     string
)

// rejectUnknown spends the time of a password comparison for a login that
// names no account, so an unknown identifier takes as long as a wrong password.
func rejectUnknown(password string) {
	unknownHashOnce.Do(func() {
		hash, err := bcrypt.GenerateFromPassword([]byte("no such account"), bcrypt.DefaultCost)
		if err == nil {
			unknownHash = string(hash)
		}
	})
	auth.VerifyPassword(unknownHash, password)
}

// requestPasswordReset queues a reset email for an already-normalized
// address. It never looks the account up on the request: the outbox does,
// so every address takes the same path.
func (m *mailer) requestPasswordReset(ctx context.Context, email string, link linker) {
	m.queue(ctx, email, outboxJob{
		purpose: PurposePasswordReset,
		prepare: func(jobCtx context.Context) (delivery, bool, error) { return m.prepareReset(jobCtx, email, link) },
	})
}

// resetOutcome is what completePasswordReset decided.
type resetOutcome struct {
	// Problem is a user-facing reason the new password cannot be used; the
	// token was not consumed.
	Problem string
	// Invalid reports that another request already used the token.
	Invalid bool
}

// completePasswordReset sets a new password for account, consuming its reset
// token row, when password follows the registration rules.
func (m *mailer) completePasswordReset(ctx context.Context, row AccountToken, account Account, password string) (resetOutcome, error) {
	if problem := passwordProblem(password); problem != "" {
		return resetOutcome{Problem: problem}, nil
	}
	won, err := m.useToken(ctx, row)
	if err != nil || !won {
		return resetOutcome{Invalid: !won && err == nil}, err
	}
	return resetOutcome{}, m.setPassword(ctx, account, password)
}

// completeVerification consumes row and marks account's email verified. It
// reports false when another request already used the token.
func (m *mailer) completeVerification(ctx context.Context, row AccountToken, account Account) (bool, error) {
	won, err := m.useToken(ctx, row)
	if err != nil || !won {
		return false, err
	}
	if account.EmailVerifiedAt.IsZero() {
		account.EmailVerifiedAt = m.cfg.now().UTC()
		if err := m.store.Update(ctx, accountMeta(), &account); err != nil {
			return false, err
		}
	}
	return true, nil
}
