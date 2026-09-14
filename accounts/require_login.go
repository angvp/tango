package accounts

import (
	"context"
	"net/url"

	"github.com/angvp/tango"
	"github.com/angvp/tango/auth"
	"github.com/angvp/tango/db"
)

// accountFromToken resolves token to its still-Active Account, or
// ok=false for a missing, invalid, expired, or now-inactive session.
// Unlike auth.SessionUser (which only resolves a session token to a raw
// user ID and has no notion of Active at all), this looks up the full
// Account row and checks Active fresh on every call — deactivating an
// Account invalidates its standing sessions immediately, not just its
// ability to log in again. See ADR 0021.
func accountFromToken(ctx context.Context, store *db.Store, token string) (Account, bool, error) {
	if token == "" {
		return Account{}, false, nil
	}

	_, sessionMeta := accountModelMetas()
	userID, ok, err := auth.SessionUser(ctx, store, sessionMeta, token)
	if err != nil {
		return Account{}, false, err
	}
	if !ok {
		return Account{}, false, nil
	}

	meta, _ := accountModelMetas()
	var rows []Account
	sqlQuery := "SELECT " +
		db.ColumnName("ID") + " AS ID, " +
		db.ColumnName("Email") + " AS Email, " +
		db.ColumnName("PasswordHash") + " AS PasswordHash, " +
		db.ColumnName("Active") + " AS Active, " +
		db.ColumnName("CreatedAt") + " AS CreatedAt FROM " +
		db.ColumnName(meta.Name) + " WHERE " + db.ColumnName("ID") + " = ?"
	if err := store.Query(ctx, &rows, sqlQuery, userID); err != nil {
		return Account{}, false, err
	}
	if len(rows) == 0 || !rows[0].Active {
		return Account{}, false, nil
	}
	return rows[0], true, nil
}

// RequireLogin wraps next so it only runs for a request carrying a valid,
// unexpired session for an Active Account — resolved fresh on every call
// via accountFromToken, so a deactivated Account's standing session stops
// working immediately. A request that fails this check (missing session,
// invalid/expired token, or a now-inactive Account — all treated
// identically, since Account has only one gate, unlike AdminUser's
// separate Active/IsStaff tiers) redirects to loginPath with a "next"
// query parameter pointing back at the original request.
//
// cookieName must match whatever accounts.New was configured with
// (DefaultSessionCookieName if using the default).
func RequireLogin(store *db.Store, cookieName string, loginPath string, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		var token string
		if cookie, err := ctx.Request().Cookie(cookieName); err == nil {
			token = cookie.Value
		}

		_, ok, err := accountFromToken(ctx.Context(), store, token)
		if err != nil {
			return err
		}
		if !ok {
			nextPath := safeAccountsNext(ctx.Request().URL.Path, "")
			target := loginPath + "?next=" + url.QueryEscape(nextPath)
			return ctx.Redirect(target)
		}

		return next(ctx)
	}
}
