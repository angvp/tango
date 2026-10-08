package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/http"
	"time"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/model"
)

// hashSecret is the SHA-256, in hex, that AccountToken stores for a token
// or a normalized address.
func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// issueToken deletes expired tokens and account's earlier tokens of
// purpose, then stores and returns a new one, valid for lifetime from now.
func issueToken(ctx context.Context, store *db.Store, account Account, purpose TokenPurpose, now time.Time, lifetime time.Duration) (string, error) {
	if err := deleteWhere[AccountToken](ctx, store, tokenMeta(), db.Condition{Field: "ExpiresAt", Op: db.OpLte, Value: now}); err != nil {
		return "", err
	}
	if err := deleteWhere[AccountToken](ctx, store, tokenMeta(),
		db.Condition{Field: "AccountID", Op: db.OpEq, Value: account.ID},
		db.Condition{Field: "Purpose", Op: db.OpEq, Value: string(purpose)}); err != nil {
		return "", err
	}
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	row := AccountToken{
		TokenHash:   hashSecret(token),
		AccountID:   account.ID,
		Purpose:     purpose,
		AddressHash: hashSecret(normalizeEmail(account.Email)),
		ExpiresAt:   now.Add(lifetime),
	}
	if err := store.Create(ctx, tokenMeta(), &row); err != nil {
		return "", err
	}
	return token, nil
}

// deleteWhere deletes every row of meta, a model of type T, matching all
// of where.
func deleteWhere[T interface{ primaryKey() int64 }](ctx context.Context, store *db.Store, meta model.ModelMeta, where ...db.Condition) error {
	var rows []T
	if err := store.List(ctx, meta, db.Query{Where: where}, &rows); err != nil {
		return err
	}
	for _, row := range rows {
		if err := store.Delete(ctx, meta, row.primaryKey()); err != nil && !errors.Is(err, db.ErrNotFound) {
			return err
		}
	}
	return nil
}

// usableToken returns token's row and account when token is an unexpired
// purpose token for an active account whose email is still the address
// the token was sent to; otherwise ok is false.
func (m *mailer) usableToken(ctx context.Context, token string, purpose TokenPurpose) (row AccountToken, account Account, ok bool, err error) {
	if token == "" {
		return row, account, false, nil
	}
	var rows []AccountToken
	query := db.Query{Where: []db.Condition{{Field: "TokenHash", Op: db.OpEq, Value: hashSecret(token)}}, Limit: 1}
	if err := m.store.List(ctx, tokenMeta(), query, &rows); err != nil || len(rows) == 0 {
		return row, account, false, err
	}
	row = rows[0]
	if row.Purpose != purpose || !m.cfg.now().Before(row.ExpiresAt) {
		return row, account, false, nil
	}
	if err := m.store.Get(ctx, accountMeta(), row.AccountID, &account); err != nil {
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

// useToken deletes row, reporting false if another request already did:
// of two concurrent uses, only one wins.
func (m *mailer) useToken(ctx context.Context, row AccountToken) (bool, error) {
	if err := m.store.Delete(ctx, tokenMeta(), row.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// tokenLink is one kind of emailed link: the form its GET shows, and what
// its POST does once the token is known to be usable. act decides itself
// when to use the token up, so a rejected form can keep it.
type tokenLink struct {
	purpose TokenPurpose
	form    *template.Template
	act     func(ctx *tango.Context, row AccountToken, account Account) error
}

// tokenFormData is a token link's form: its CSRF token and any error.
type tokenFormData struct {
	CSRFToken string
	Error     string
}

// tokenLinkView serves link at its ?token=… URL. GET shows the form and
// changes nothing, so a mail scanner following the link uses nothing up;
// POST acts. Every response keeps the token out of referrers and caches,
// and every unusable token gets the same invalid-link page.
func (m *mailer) tokenLinkView(link tokenLink) tango.View {
	return func(ctx *tango.Context) error {
		ctx.ResponseWriter().Header().Set("Referrer-Policy", "no-referrer")
		ctx.ResponseWriter().Header().Set("Cache-Control", "no-store")
		switch ctx.Request().Method {
		case http.MethodGet:
			return m.showTokenLink(ctx, link)
		case http.MethodPost:
			return m.actOnTokenLink(ctx, link)
		default:
			return methodNotAllowed(ctx)
		}
	}
}

func (m *mailer) showTokenLink(ctx *tango.Context, link tokenLink) error {
	csrf, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
	if err != nil {
		return err
	}
	if _, _, ok, err := m.usableToken(ctx.Context(), ctx.Query("token"), link.purpose); err != nil || !ok {
		if err != nil {
			return err
		}
		return invalidLink(ctx)
	}
	return render(ctx, http.StatusOK, link.form, tokenFormData{CSRFToken: csrf})
}

func (m *mailer) actOnTokenLink(ctx *tango.Context, link tokenLink) error {
	if err := ctx.Request().ParseForm(); err != nil {
		return err
	}
	if !verifyPreSessionCSRF(ctx.Request()) {
		return forbiddenCSRF(ctx)
	}
	row, account, ok, err := m.usableToken(ctx.Context(), ctx.Query("token"), link.purpose)
	if err != nil {
		return err
	}
	if !ok {
		return invalidLink(ctx)
	}
	return link.act(ctx, row, account)
}

// invalidLink is the one answer to every unusable token, so it never says
// whether the token was unknown, expired, used, replaced, or sent to an
// address the account no longer has.
func invalidLink(ctx *tango.Context) error {
	return render(ctx, http.StatusBadRequest, invalidLinkTemplate, nil)
}
