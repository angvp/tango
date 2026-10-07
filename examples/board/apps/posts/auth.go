package posts

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/angvp/tango"
	"github.com/angvp/tango/accounts"
	"github.com/angvp/tango/auth/jwt"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
	"github.com/angvp/tango/ratelimit"
)

// tokenAccount returns the account behind the request's API token. A token
// stays valid until it expires, so the account is re-checked here: a token
// for a deleted or deactivated account stops working immediately.
func tokenAccount(ctx *tango.Context, store *db.Store, accountMeta model.ModelMeta) (accounts.Account, bool, error) {
	claims, ok := jwt.FromContext(ctx.Context())
	if !ok {
		return accounts.Account{}, false, nil
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return accounts.Account{}, false, nil
	}
	var account accounts.Account
	err = store.Get(ctx.Context(), accountMeta, id, &account)
	if errors.Is(err, db.ErrNotFound) {
		return accounts.Account{}, false, nil
	}
	if err != nil {
		return accounts.Account{}, false, err
	}
	return account, account.Active, nil
}

// accountKey rate-limits per account when the request carries a verified
// token, and per client IP when it doesn't.
func accountKey(r *http.Request) (string, error) {
	if claims, ok := jwt.FromContext(r.Context()); ok {
		return "account:" + claims.Subject, nil
	}
	return ratelimit.RemoteIPKey()(r)
}

func unauthorized(ctx *tango.Context) error {
	return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}
