package accounts

import (
	"time"

	"github.com/angvp/tango/model"
)

// Account is the accounts app's own Application user model — a plain
// identity, credential, activity state, and timestamp. tanGO deliberately
// ships no permission-shaped field (no IsStaff, Role, Group, or
// Permission): an app wanting roles or permissions writes its own View
// wrapper against its own domain data, including Account itself if this
// app is installed. See docs/guides/accounts.md.
//
// Active gates both a fresh login and every subsequent request through an
// already-valid session — not just login, unlike auth.SessionUser, which
// has no notion of Active at all.
//
// EmailVerifiedAt is when the owner proved they receive mail at Email; the
// zero time means unverified. It gates nothing by itself: an app that needs
// a verified email guards its Views with RequireVerified. Whoever changes
// Email must clear it.
type Account struct {
	ID              int64  `tango:"pk"`
	Email           string `tango:"unique"`
	PasswordHash    string
	Active          bool
	CreatedAt       time.Time
	EmailVerifiedAt time.Time
}

// TokenPurpose is what an AccountToken proves.
type TokenPurpose string

// The two purposes an AccountToken can have.
const (
	PurposePasswordReset     TokenPurpose = "password_reset"
	PurposeEmailVerification TokenPurpose = "email_verification"
)

// AccountToken is one outstanding emailed token: a password reset or an
// email verification for one Account. Only hashes are stored: TokenHash is
// the SHA-256 of the token the email carried, and AddressHash the SHA-256
// of the normalized address it was sent to, so a token stops working if
// the account's Email changes. A token is single-use and expires at
// ExpiresAt.
type AccountToken struct {
	ID          int64  `tango:"pk"`
	TokenHash   string `tango:"unique"`
	AccountID   int64  `tango:"fk=Account,index"`
	Purpose     TokenPurpose
	AddressHash string
	ExpiresAt   time.Time
}

// AccountSession is one active login for an Account: a session token, which
// Account it belongs to, and a fixed expiry. Its shape is not a free
// choice — it satisfies auth.CreateSession's reflection contract (Token
// string, UserID foreign-keyed to Account, ExpiresAt time.Time), mirroring
// AdminSession's shape without being built on the same code path admin
// uses. Deleting a session's row logs out that one login without affecting
// any other session belonging to the same Account.
type AccountSession struct {
	ID        int64  `tango:"pk"`
	Token     string `tango:"unique"`
	UserID    int64  `tango:"fk=Account,index"`
	ExpiresAt time.Time
}

// accountModelMetas returns Account/AccountSession's model metadata,
// independent of any project's own model registry — accounts's own views
// operate directly against store, without depending on the host project's
// full registration having already run, mirroring admin's own
// adminModelMetas.
func accountModelMetas() (accountMeta model.ModelMeta, sessionMeta model.ModelMeta) {
	registry := model.NewRegistry()
	// Account and AccountSession are known-good models (accounts.New
	// registers them the same way at app registration time), so these
	// errors cannot occur here in practice.
	_ = registry.Register(Account{})
	_ = registry.Register(AccountSession{})
	_ = registry.Register(AccountToken{})
	accountMeta, _ = registry.Get("Account")
	sessionMeta, _ = registry.Get("AccountSession")
	return accountMeta, sessionMeta
}
