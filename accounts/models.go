package accounts

import (
	"time"

	"github.com/angvp/tango/model"
)

// Account is the accounts app's own Application user model — a plain
// identity, credential, activity state, and timestamp. v0.1 deliberately
// ships no permission-shaped field (no IsStaff, Role, Group, or
// Permission): an app wanting roles or permissions writes its own View
// wrapper against its own domain data, including Account itself if this
// app is installed. See docs/guides/accounts.md.
//
// Active gates both a fresh login and every subsequent request through an
// already-valid session — not just login, unlike auth.SessionUser, which
// has no notion of Active at all.
type Account struct {
	ID           int64  `tango:"pk"`
	Email        string `tango:"unique"`
	PasswordHash string
	Active       bool
	CreatedAt    time.Time
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
	accountMeta, _ = registry.Get("Account")
	sessionMeta, _ = registry.Get("AccountSession")
	return accountMeta, sessionMeta
}
