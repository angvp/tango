package accounts

import (
	"context"
	"fmt"
	"net/http"

	"github.com/angvp/tango"
	"github.com/angvp/tango/db"
	"github.com/angvp/tango/internal/security"
	tangomail "github.com/angvp/tango/mail"
)

// mailer is what the mail flows share: the validated MailConfig, the
// outbox and the per-address cooldown.
type mailer struct {
	store    *db.Store
	cfg      accountsConfig
	from     string
	baseURL  string
	outbox   *outbox
	cooldown *cooldown
}

// mayEmail reports whether email may be sent another purpose email now,
// recording it if so. Unknown addresses are counted too, so the cooldown
// never depends on whether an account exists.
func (m *mailer) mayEmail(email string, purpose TokenPurpose) bool {
	return m.cooldown.allow(string(purpose)+"\x00"+email, m.cfg.now())
}

// passwordResetView handles GET (the form asking for a link) and POST
// (queue the link) for /accounts/password-reset/. POST answers the same
// way for every address: the outbox, not the request, looks the account
// up, so neither the response nor its timing says whether one exists.
func passwordResetView(m *mailer, limiter *security.RateLimiter) tango.View {
	return func(ctx *tango.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet:
			token, err := ensurePreSessionCSRFCookie(ctx.ResponseWriter(), ctx.Request())
			if err != nil {
				return err
			}
			return render(ctx, http.StatusOK, passwordResetRequestTemplate, passwordResetRequestPageData{CSRFToken: token})

		case http.MethodPost:
			if err := ctx.Request().ParseForm(); err != nil {
				return err
			}
			if !verifyPreSessionCSRF(ctx.Request()) {
				return forbiddenCSRF(ctx)
			}
			key := rateLimitKey(ctx.Request())
			if !limiter.Allow(key) {
				return tooManyRequests(ctx)
			}
			limiter.RecordFailure(key)

			email := normalizeEmail(ctx.Request().PostForm.Get("email"))
			if email == "" {
				csrfCookie, _ := ctx.Request().Cookie(preSessionCSRFCookieName)
				return render(ctx, http.StatusBadRequest, passwordResetRequestTemplate, passwordResetRequestPageData{
					CSRFToken: csrfCookie.Value,
					Error:     "Email is required.",
				})
			}
			if m.mayEmail(email, PurposePasswordReset) {
				m.outbox.enqueue(ctx.Context(), outboxJob{
					purpose: PurposePasswordReset,
					prepare: func(jobCtx context.Context) (delivery, bool, error) { return m.prepareReset(jobCtx, email) },
				})
			}
			return render(ctx, http.StatusOK, passwordResetSentTemplate, nil)

		default:
			return methodNotAllowed(ctx)
		}
	}
}

// prepareReset issues a reset token for email's account and returns its
// email, or ok=false when there is no active account for email.
func (m *mailer) prepareReset(ctx context.Context, email string) (delivery, bool, error) {
	account, ok, err := findAccountByEmail(ctx, m.store, email)
	if err != nil || !ok || !account.Active {
		return delivery{}, false, err
	}
	token, err := issueToken(ctx, m.store, account, PurposePasswordReset, m.cfg.now(), resetTokenLifetime)
	if err != nil {
		return delivery{}, false, err
	}
	link := m.baseURL + "/accounts/password-reset/confirm/?token=" + token
	return delivery{
		message: tangomail.Message{
			From:    m.from,
			To:      account.Email,
			Subject: "Reset your password",
			Text: fmt.Sprintf("Someone asked to reset the password for this email address.\n\n"+
				"To choose a new password, open this link within 1 hour:\n\n%s\n\n"+
				"If you didn't ask, ignore this email: your password stays the same.\n", link),
		},
		secret: token,
	}, true, nil
}
