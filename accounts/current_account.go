package accounts

import (
	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
)

// CurrentAccountID returns the requesting Account's ID for a host's own
// View, or ok=false for a missing, invalid, expired, or now-inactive
// session — consistent with RequireLogin's strict Active check (ticket
// 07). Thin sugar over the same accountFromToken RequireLogin uses.
//
// cookieName must match whatever accounts.New was configured with
// (DefaultSessionCookieName if using the default) — mirroring
// auth.CurrentUserID's own explicit-cookieName shape, since this helper
// has no other way to know it.
func CurrentAccountID(ctx *tango.Context, store *db.Store, cookieName string) (int64, bool, error) {
	account, ok, err := currentAccount(ctx, store, cookieName)
	if err != nil || !ok {
		return 0, false, err
	}
	return account.ID, true, nil
}

// CurrentAccount returns the requesting Account's full row for a host's
// own View, or ok=false under the same conditions as CurrentAccountID.
func CurrentAccount(ctx *tango.Context, store *db.Store, cookieName string) (Account, bool, error) {
	return currentAccount(ctx, store, cookieName)
}

// currentAccount is the shared implementation behind CurrentAccountID and
// CurrentAccount.
func currentAccount(ctx *tango.Context, store *db.Store, cookieName string) (Account, bool, error) {
	var token string
	if cookie, err := ctx.Request().Cookie(cookieName); err == nil {
		token = cookie.Value
	}
	return accountFromToken(ctx.Context(), store, token)
}
