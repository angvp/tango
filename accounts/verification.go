package accounts

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	tangomail "github.com/angvp/tango/mail"
)

// queueVerification queues a verification email for account, unless its
// address is in its cooldown.
func (m *mailer) queueVerification(ctx context.Context, account Account) {
	if !m.mayEmail(normalizeEmail(account.Email), PurposeEmailVerification) {
		return
	}
	id := account.ID
	m.outbox.enqueue(ctx, outboxJob{
		purpose: PurposeEmailVerification,
		prepare: func(jobCtx context.Context) (delivery, bool, error) { return m.prepareVerification(jobCtx, id) },
	})
}

// prepareVerification issues a verification token for the account and
// returns its email, or ok=false when the account is gone, inactive or
// already verified.
func (m *mailer) prepareVerification(ctx context.Context, accountID int64) (delivery, bool, error) {
	accountMeta, _, _ := accountModelMetas()
	var account Account
	if err := m.store.Get(ctx, accountMeta, accountID, &account); err != nil {
		if isNotFound(err) {
			return delivery{}, false, nil
		}
		return delivery{}, false, err
	}
	if !account.Active || !account.EmailVerifiedAt.IsZero() {
		return delivery{}, false, nil
	}
	token, err := issueToken(ctx, m.store, account, PurposeEmailVerification, m.cfg.now(), verificationTokenLifetime)
	if err != nil {
		return delivery{}, false, err
	}
	link := m.baseURL + "/accounts/verify/?token=" + token
	return delivery{
		message: tangomail.Message{
			From:    m.from,
			To:      account.Email,
			Subject: "Confirm your email address",
			Text: fmt.Sprintf("Please confirm that this is your email address by opening this link within 24 hours:\n\n%s\n\n"+
				"If you didn't create an account, ignore this email.\n", link),
		},
		secret: token,
	}, true, nil
}

// verifyView handles GET (a "Confirm my email" button) and POST (verify)
// for /accounts/verify/?token=…. Only POST uses the token, so a mail
// scanner following the link verifies nothing.
func verifyView(m *mailer) tango.View {
	return func(ctx *tango.Context) error {
		tokenPage(ctx.ResponseWriter())
		token := ctx.Query("token")
		switch ctx.Request().Method {
		case http.MethodGet:
			csrf, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			if _, _, ok, err := m.usableToken(ctx.Context(), token, PurposeEmailVerification); err != nil {
				return err
			} else if !ok {
				return invalidLink(ctx)
			}
			return render(ctx, http.StatusOK, verifyTemplate, verifyPageData{CSRFToken: csrf})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyPreSessionCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}
			row, account, ok, err := m.usableToken(ctx.Context(), token, PurposeEmailVerification)
			if err != nil {
				return err
			}
			if !ok {
				return invalidLink(ctx)
			}
			if won, err := m.consumeToken(ctx.Context(), row); err != nil {
				return err
			} else if !won {
				return invalidLink(ctx)
			}
			if account.EmailVerifiedAt.IsZero() {
				account.EmailVerifiedAt = m.cfg.now().UTC()
				accountMeta, _, _ := accountModelMetas()
				if err := m.store.Update(ctx.Context(), accountMeta, &account); err != nil {
					return err
				}
			}
			return render(ctx, http.StatusOK, verifiedTemplate, nil)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// resendVerificationView handles POST /accounts/verify/resend/ for a
// logged-in account: it queues a new verification email, subject to the
// per-address cooldown, and answers the same way whether or not one was
// queued.
func resendVerificationView(m *mailer) tango.View {
	return func(ctx *tango.Context) error {
		if err := ctx.Request().ParseForm(); err != nil {
			return err
		}
		account, ok, err := currentAccount(ctx, m.store, m.cfg.sessionCookieName)
		if err != nil {
			return err
		}
		if !ok {
			return ctx.Redirect("/accounts/login/")
		}
		if !verifyPreSessionCSRF(ctx.Request()) {
			return forbiddenCSRF(ctx)
		}
		if account.EmailVerifiedAt.IsZero() {
			m.queueVerification(ctx.Context(), account)
		}
		return render(ctx, http.StatusOK, verificationSentTemplate, nil)
	}
}

// RequireVerified wraps next so it only runs for a logged-in account with a
// Verified email. Like RequireLogin, a request without a valid session for
// an active account redirects to loginPath with a "next" parameter. A
// logged-in account that hasn't verified its email gets a 403 HTML page
// with a button to send a new verification link (which needs WithMail).
// Verification never stops an account from logging in; this guard is the
// only thing that acts on it.
func RequireVerified(store *db.Store, cookieName string, loginPath string, next tango.View) tango.View {
	return func(ctx *tango.Context) error {
		account, ok, err := currentAccount(ctx, store, cookieName)
		if err != nil {
			return err
		}
		if !ok {
			nextPath := safeAccountsNext(ctx.Request().URL.Path, "")
			return ctx.Redirect(loginPath + "?next=" + url.QueryEscape(nextPath))
		}
		if account.EmailVerifiedAt.IsZero() {
			csrf, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusForbidden, unverifiedTemplate, verifyPageData{CSRFToken: csrf})
		}
		return next(ctx)
	}
}
