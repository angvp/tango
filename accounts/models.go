package accounts

import (
	"fmt"
	"sync"
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

// modelMetas is the metadata of accounts' own models, independent of any
// project's model registry: accounts' views operate directly against the
// store without waiting for the host's registration to run, mirroring
// admin's own adminModelMetas. The models are known-good (accounts.New
// registers the same ones), so a failure here is a bug in this package.
var modelMetas = sync.OnceValue(func() map[string]model.ModelMeta {
	registry := model.NewRegistry()
	metas := map[string]model.ModelMeta{}
	for name, m := range map[string]any{"Account": Account{}, "AccountSession": AccountSession{}, "AccountToken": AccountToken{}} {
		if err := registry.Register(m); err != nil {
			panic(fmt.Sprintf("accounts: built-in model %s is invalid: %v", name, err))
		}
		meta, ok := registry.Get(name)
		if !ok {
			panic("accounts: built-in model " + name + " didn't register")
		}
		metas[name] = meta
	}
	return metas
})

func accountMeta() model.ModelMeta { return modelMetas()["Account"] }
func sessionMeta() model.ModelMeta { return modelMetas()["AccountSession"] }
func tokenMeta() model.ModelMeta   { return modelMetas()["AccountToken"] }

func (s AccountSession) primaryKey() int64 { return s.ID }
func (t AccountToken) primaryKey() int64   { return t.ID }
