package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
)

// queueVerification queues a verification email for account, unless its
// address is in its cooldown.
func (m *mailer) queueVerification(ctx context.Context, account Account) {
	id := account.ID
	m.queue(ctx, normalizeEmail(account.Email), outboxJob{
		purpose: PurposeEmailVerification,
		prepare: func(jobCtx context.Context) (delivery, bool, error) { return m.prepareVerification(jobCtx, id) },
	})
}

// prepareVerification issues a verification token for the account and
// returns its email, or ok=false when the account is gone, inactive or
// already verified.
func (m *mailer) prepareVerification(ctx context.Context, accountID int64) (delivery, bool, error) {
	var account Account
	if err := m.store.Get(ctx, accountMeta(), accountID, &account); err != nil {
		if errors.Is(err, db.ErrNotFound) {
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
	return m.emailWithLink(account, token, "/accounts/verify/", "Confirm your email address",
		"Please confirm that this is your email address by opening this link within 24 hours:\n\n%s\n\n"+
			"If you didn't create an account, ignore this email.\n"), true, nil
}

// verifyLink is /accounts/verify/: its form is a "Confirm my email"
// button, and confirming sets EmailVerifiedAt.
func (m *mailer) verifyLink() tokenLink {
	return tokenLink{
		purpose: PurposeEmailVerification,
		form:    verifyTemplate,
		act: func(ctx *tango.Context, row AccountToken, account Account) error {
			won, err := m.completeVerification(ctx.Context(), row, account)
			if err != nil {
				return err
			}
			if !won {
				return invalidLink(ctx)
			}
			return render(ctx, http.StatusOK, verifiedTemplate, nil)
		},
	}
}

// resendVerificationView handles POST /accounts/verify/resend/ for a
// logged-in account: it queues a new verification email, subject to the
// per-IP limit and the per-address cooldown, and answers the same way
// whether or not one was queued.
func resendVerificationView(m *mailer, limiter *security.RateLimiter) tango.View {
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
		key := m.cfg.clientKey(ctx.Request())
		if !limiter.Allow(key) {
			return tooManyRequests(ctx)
		}
		limiter.RecordFailure(key)
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
			return render(ctx, http.StatusForbidden, unverifiedTemplate, tokenFormData{CSRFToken: csrf})
		}
		return next(ctx)
	}
}
