package accounts

import (
	"context"
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// usableToken returns token's row and account when token is an unexpired
// purpose token for an active account whose email is still the address
// the token was sent to; otherwise ok is false.
func (m *mailer) usableToken(ctx context.Context, token string, purpose TokenPurpose) (row AccountToken, account Account, ok bool, err error) {
	if token == "" {
		return row, account, false, nil
	}
	accountMeta, _, tokenMeta := accountModelMetas()
	var rows []AccountToken
	query := db.Query{Where: []db.Condition{{Field: "TokenHash", Op: db.OpEq, Value: hashSecret(token)}}, Limit: 1}
	if err := m.store.List(ctx, tokenMeta, query, &rows); err != nil || len(rows) == 0 {
		return row, account, false, err
	}
	row = rows[0]
	if row.Purpose != purpose || !m.cfg.now().Before(row.ExpiresAt) {
		return row, account, false, nil
	}
	if err := m.store.Get(ctx, accountMeta, row.AccountID, &account); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return row, account, false, nil
		}
		return row, account, false, err
	}
	if !account.Active || hashSecret(normalizeEmail(account.Email)) != row.AddressHash {
		return row, account, false, nil
	}
	return row, account, true, nil
}

func isNotFound(err error) bool { return errors.Is(err, db.ErrNotFound) }

// consumeToken deletes row, reporting false if another request already
// did: of two concurrent uses, only one wins.
func (m *mailer) consumeToken(ctx context.Context, row AccountToken) (bool, error) {
	_, _, tokenMeta := accountModelMetas()
	if err := m.store.Delete(ctx, tokenMeta, row.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// tokenPage marks a response that carries a token in its URL: no referrer
// leaves it, and no cache keeps it.
func tokenPage(w http.ResponseWriter) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
}

// invalidLink is the one answer to every unusable token, so it never says
// whether the token was unknown, expired, used or for another address.
func invalidLink(ctx *tango.Context) error {
	return render(ctx, http.StatusBadRequest, invalidLinkTemplate, nil)
}

// passwordResetConfirmView handles GET (the new-password form) and POST
// (set the password) for /accounts/password-reset/confirm/?token=…. Only
// POST uses the token up, so a mail scanner following the link changes
// nothing. A completed reset ends every session and token the account has
// and verifies its email.
func passwordResetConfirmView(m *mailer) tango.View {
	return func(ctx *tango.Context) error {
		tokenPage(ctx.ResponseWriter())
		token := ctx.Query("token")
		switch ctx.Request().Method {
		case http.MethodGet:
			csrf, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			if _, _, ok, err := m.usableToken(ctx.Context(), token, PurposePasswordReset); err != nil {
				return err
			} else if !ok {
				return invalidLink(ctx)
			}
			return render(ctx, http.StatusOK, passwordResetConfirmTemplate, passwordResetConfirmPageData{CSRFToken: csrf})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyPreSessionCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}
			row, account, ok, err := m.usableToken(ctx.Context(), token, PurposePasswordReset)
			if err != nil {
				return err
			}
			if !ok {
				return invalidLink(ctx)
			}
			password := ctx.Request().PostForm.Get("password")
			if problem := passwordProblem(password); problem != "" {
				csrfCookie, _ := ctx.Request().Cookie(preSessionCSRFCookieName)
				return render(ctx, http.StatusBadRequest, passwordResetConfirmTemplate, passwordResetConfirmPageData{CSRFToken: csrfCookie.Value, Error: problem})
			}
			if won, err := m.consumeToken(ctx.Context(), row); err != nil {
				return err
			} else if !won {
				return invalidLink(ctx)
			}
			if err := m.setPassword(ctx.Context(), account, password); err != nil {
				return err
			}
			return ctx.Redirect("/accounts/login/")

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// setPassword sets account's password, verifies its email if it wasn't,
// and deletes every token and session the account has.
func (m *mailer) setPassword(ctx context.Context, account Account, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	account.PasswordHash = string(hash)
	if account.EmailVerifiedAt.IsZero() {
		account.EmailVerifiedAt = m.cfg.now().UTC()
	}
	accountMeta, sessionMeta, tokenMeta := accountModelMetas()
	if err := m.store.Update(ctx, accountMeta, &account); err != nil {
		return err
	}
	if err := deleteAll[AccountToken](ctx, m.store, tokenMeta, "AccountID", account.ID, func(r AccountToken) int64 { return r.ID }); err != nil {
		return err
	}
	return deleteAll[AccountSession](ctx, m.store, sessionMeta, "UserID", account.ID, func(r AccountSession) int64 { return r.ID })
}

// deleteAll deletes every row of meta whose field equals value.
func deleteAll[T any](ctx context.Context, store *db.Store, meta model.ModelMeta, field string, value any, id func(T) int64) error {
	var rows []T
	if err := store.List(ctx, meta, db.Query{Where: []db.Condition{{Field: field, Op: db.OpEq, Value: value}}}, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		if err := store.Delete(ctx, meta, id(row)); err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	return nil
}
